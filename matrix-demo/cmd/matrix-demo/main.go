// matrix-demo runs one reproducible local-XOR workload in its own directory.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mbb200291/go-occ-z-usecase/matrix-demo/internal/matrixdemo"
	"github.com/mbb200291/go-occ-z/occ"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

type event struct {
	ID       int    `json:"id"`
	Kind     string `json:"kind"`
	Attempts int64  `json:"attempts"`
	Values   []int  `json:"values,omitempty"`
	Error    string `json:"error,omitempty"`
}

func execute(m occ.Manager, c *matrixdemo.Command) event {
	out, err := m.Run([]occ.Command{c})
	e := event{ID: c.ID, Kind: c.Kind, Attempts: c.Attempts.Load()}
	if err != nil {
		e.Error = err.Error()
		return e
	}
	if c.IsReadOnly() {
		for _, v := range out[0].([]byte) {
			e.Values = append(e.Values, int(v))
		}
	}
	return e
}
func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("matrix-demo", flag.ContinueOnError)
	var c matrixdemo.Config
	flags.IntVar(&c.Size, "n", 8, "matrix side length")
	flags.IntVar(&c.Blocks, "blocks", 24, "random local XOR blocks (local correction blocks are added)")
	flags.IntVar(&c.BlockSize, "block-size", 2, "maximum block side, smaller than n")
	flags.IntVar(&c.Reads, "reads", 32, "number of subset read commands")
	flags.IntVar(&c.ReadSize, "read-size", 8, "cells per subset read")
	flags.Int64Var(&c.Seed, "seed", 42, "reproducible command set and shuffle")
	workers := flags.Int("workers", 8, "parallel command workers")
	interval := flags.Duration("interval", 200*time.Millisecond, "interval between full-matrix read commands for display")
	pause := flags.Duration("pause", 15*time.Millisecond, "pause after each worker command; 0 for throughput runs")
	dir := flags.String("dir", "", "empty output directory; default creates a temporary directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *workers < 1 || *interval <= 0 || *pause < 0 {
		return fmt.Errorf("workers and interval must be positive; pause must be nonnegative")
	}
	jobs, err := matrixdemo.Generate(c)
	if err != nil {
		return err
	}
	if *dir == "" {
		*dir, err = os.MkdirTemp("", "occ-matrix-")
	} else {
		*dir, err = filepath.Abs(*dir)
		if err == nil {
			err = os.MkdirAll(*dir, 0755)
		}
	}
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(*dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("output directory must be empty: %s", *dir)
	}
	// OCC uses cwd-relative journals. This dedicated demo process never changes it again.
	if err := os.Chdir(*dir); err != nil {
		return err
	}
	save := func(path string, v any) error {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(path, append(data, '\n'), 0600)
	}
	if err := save("commands.json", struct {
		Config matrixdemo.Config
		Jobs   []matrixdemo.Job
	}{c, jobs}); err != nil {
		return err
	}
	adapter, err := matrixdemo.Create("matrix.csv", c.Size)
	if err != nil {
		return err
	}
	defer adapter.Close()
	initial, err := os.ReadFile("matrix.csv")
	if err != nil {
		return err
	}
	if err := os.WriteFile("initial.csv", initial, 0600); err != nil {
		return err
	}
	m, err := occ.NewManager(adapter, occ.Config{Retry: &occ.RetryOptions{MaxRetries: 128, BaseDelay: time.Millisecond, MaxDelay: 50 * time.Millisecond, Jitter: true}})
	if err != nil {
		return err
	}
	logFile, err := os.Create("events.jsonl")
	if err != nil {
		return err
	}
	defer logFile.Close()
	log := json.NewEncoder(logFile)
	writes := len(jobs) - c.Reads
	header := fmt.Sprintf("directory: %s\n%d x %d | workers=%d | local XOR commands=%d | subset reads=%d (%d cells each) | seed=%d\n", *dir, c.Size, c.Size, *workers, writes, c.Reads, c.ReadSize, c.Seed)
	live := liveOutput(out)
	if !live {
		fmt.Fprint(out, header)
	}
	snapshotCount := 0
	snapshot := func(label string) ([]int, error) {
		e := execute(m, matrixdemo.FullRead(c.Size))
		e.ID = -1
		e.Kind = "display-read"
		if err := log.Encode(e); err != nil {
			return nil, err
		}
		if e.Error != "" {
			return nil, fmt.Errorf("display read: %s", e.Error)
		}
		snapshotCount++
		if err := drawMatrix(out, live, header, label, snapshotCount, e.Attempts, c.Size, e.Values); err != nil {
			return nil, err
		}
		return e.Values, nil
	}
	if _, err := snapshot("initial"); err != nil {
		return err
	}
	tasks := make(chan matrixdemo.Job, len(jobs))
	for _, j := range jobs {
		tasks <- j
	}
	close(tasks)
	results := make(chan event, len(jobs))
	var wg sync.WaitGroup
	var stop atomic.Bool
	start := time.Now()
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range tasks {
				if stop.Load() {
					return
				}
				e := execute(m, &matrixdemo.Command{Job: j})
				results <- e
				if e.Error != "" {
					stop.Store(true)
					return
				}
				if *pause > 0 {
					time.Sleep(*pause)
				}
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	completed, attempts := 0, int64(0)
	var firstErr error
	for results != nil {
		select {
		case e, ok := <-results:
			if !ok {
				results = nil
				continue
			}
			attempts += e.Attempts
			if err := log.Encode(e); err != nil && firstErr == nil {
				firstErr = err
				stop.Store(true)
			}
			if e.Error != "" {
				if firstErr == nil {
					firstErr = fmt.Errorf("command %d: %s", e.ID, e.Error)
				}
			} else {
				completed++
			}
		case <-ticker.C:
			if firstErr == nil {
				if _, err := snapshot("in progress"); err != nil {
					firstErr = err
					stop.Store(true)
				}
			}
		}
	}
	if firstErr != nil {
		return firstErr
	}
	values, err := snapshot("final")
	if err != nil {
		return err
	}
	if completed != len(jobs) {
		return fmt.Errorf("completed %d of %d commands", completed, len(jobs))
	}
	for i, v := range values {
		if v != 1 {
			return fmt.Errorf("cell %d is %d, expected 1", i, v)
		}
	}
	// Independently check the on-disk CSV shape and final value, after all commands.
	data, err := os.ReadFile("matrix.csv")
	if err != nil {
		return err
	}
	expected := strings.Repeat(strings.Repeat("1,", c.Size-1)+"1\n", c.Size)
	if !bytes.Equal(data, []byte(expected)) {
		return fmt.Errorf("final CSV is not an all-ones matrix")
	}
	elapsed := time.Since(start)
	summary := struct {
		Commands            int
		Attempts, Conflicts int64
		Snapshots           int
		Elapsed             string
	}{completed, attempts, attempts - int64(completed), snapshotCount, elapsed.String()}
	if err := save("result.json", summary); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nPASS: all %d cells are 1; commands=%d attempts=%d conflicts=%d elapsed=%s\nDisplay reads are additional OCC transactions; timing includes them and worker pauses.\n", len(values), completed, attempts, summary.Conflicts, elapsed.Round(time.Millisecond))
	return logFile.Sync()
}
