package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/mbb200291/go-occ-z-usecase/matrix-demo/internal/matrixdemo"
	"github.com/mbb200291/go-occ-z/occ"
)

type measurement struct {
	Seconds        float64
	CleanupSeconds float64
	Attempts       int64
	ExtraAttempts  int64
	Cells          int
}

// The delay stays inside Execute, before OCC validation, on every attempt.
type benchmarkCommand struct {
	matrixdemo.Command
	delay time.Duration
}

func (c *benchmarkCommand) Execute(ctx occ.Context) (any, error) {
	outcome, err := c.Command.Execute(ctx)
	if err == nil && c.delay > 0 {
		time.Sleep(c.delay)
	}
	return outcome, err
}

// One process per sample: OCC owns cwd-relative journals until process exit.
func measure(c sampleConfig) (measurement, error) {
	var m measurement
	jobs, expected, k := plan(c)
	m.Cells = k
	s, err := newStorage(c.Backend, c.N)
	if err != nil {
		return m, err
	}
	mgr, err := occ.NewManager(s, occ.Config{Retry: &occ.RetryOptions{MaxRetries: c.MaxRetries, BaseDelay: time.Millisecond, MaxDelay: 100 * time.Millisecond, Jitter: c.Jitter}})
	if err != nil {
		return m, err
	}
	// Warm every target equally outside timing; these are OS-cache-warm file runs.
	for i := range expected {
		if _, err := s.Read(fmt.Sprintf("%d,%d", i/c.N, i%c.N)); err != nil {
			return m, err
		}
	}
	commands := make([]*benchmarkCommand, len(jobs))
	for i, job := range jobs {
		commands[i] = &benchmarkCommand{Command: matrixdemo.Command{Job: job}, delay: c.WorkDelay}
	}
	queue := make(chan *benchmarkCommand, len(commands))
	for _, cmd := range commands {
		queue <- cmd
	}
	close(queue)
	startGate := make(chan struct{})
	errs := make(chan error, len(commands))
	var ready, finished sync.WaitGroup
	for range c.Workers {
		ready.Add(1)
		finished.Add(1)
		go func() {
			defer finished.Done()
			ready.Done()
			<-startGate
			for cmd := range queue {
				outcomes, err := mgr.Run([]occ.Command{cmd})
				if err != nil {
					errs <- err
					continue
				}
				if len(outcomes) != 1 {
					errs <- fmt.Errorf("unexpected outcomes length")
					continue
				}
				if cmd.Kind == "read" {
					bits, ok := outcomes[0].([]byte)
					if !ok || len(bits) != k {
						errs <- fmt.Errorf("invalid read result")
						continue
					}
					for _, v := range bits {
						if v > 1 {
							errs <- fmt.Errorf("invalid read bit")
							break
						}
					}
				}
			}
		}()
	}
	ready.Wait()
	start := time.Now()
	close(startGate)
	finished.Wait()
	m.Seconds = time.Since(start).Seconds()
	close(errs)
	for err := range errs {
		if err != nil {
			return m, err
		}
	}
	for _, cmd := range commands {
		m.Attempts += cmd.Attempts.Load()
	}
	m.ExtraAttempts = m.Attempts - int64(len(commands))
	for i, want := range expected {
		v, err := s.Read(fmt.Sprintf("%d,%d", i/c.N, i%c.N))
		if err != nil || v != want {
			return m, fmt.Errorf("cell %d: got %v, expected %d, error %v", i, v, want, err)
		}
	}
	// Commit cleanup is asynchronous. Drain it before process exit, outside the
	// Run-latency timing, so it cannot spill into another sample or be killed early.
	cleanup := time.Now()
	deadline := cleanup.Add(30 * time.Second)
	for {
		entries, err := os.ReadDir(".occ")
		if err != nil {
			return m, err
		}
		pending := false
		for _, e := range entries {
			if e.IsDir() {
				pending = true
				break
			}
		}
		if !pending {
			break
		}
		if time.Now().After(deadline) {
			return m, fmt.Errorf("journal cleanup timeout")
		}
		time.Sleep(time.Millisecond)
	}
	m.CleanupSeconds = time.Since(cleanup).Seconds()
	return m, nil
}
