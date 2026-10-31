package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gofrs/flock"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed plot.py
var plotScript string

type row struct {
	Backend                                     string
	ReadParts                                   int
	Coverage                                    float64
	Cells                                       int
	Serial, Parallel, Speedup, Low, High, Extra float64
}

func number(n float64) string { return strconv.FormatFloat(n, 'g', 10, 64) }
func quantile(v []float64, q float64) float64 {
	a := append([]float64(nil), v...)
	sort.Float64s(a)
	p := q * float64(len(a)-1)
	i := int(p)
	return a[i] + (a[min(i+1, len(a)-1)]-a[i])*(p-float64(i))
}
func child(c sampleConfig, root string, timeout time.Duration) (measurement, error) {
	var m measurement
	dir, err := os.MkdirTemp(root, "sample-")
	if err != nil {
		return m, err
	}
	defer os.RemoveAll(dir)
	exe, err := os.Executable()
	if err != nil {
		return m, err
	}
	data, _ := json.Marshal(c)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-sample", string(data))
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return m, fmt.Errorf("sample failed: %w (context: %v): %s", err, ctx.Err(), stderr.String())
	}
	if err := json.Unmarshal(out, &m); err != nil {
		return m, err
	}
	return m, nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	resume := flag.Bool("resume", false, "reuse completed samples in output directory; settings must match")
	sample := flag.String("sample", "", "internal: one isolated sample")
	n := flag.Int("n", 16, "matrix side")
	requests := flag.Int("requests", 100, "requests per sample, multiple of 10")
	workers := flag.Int("workers", 8, "parallel workers")
	repeats := flag.Int("repeats", 3, "measured pairs per point")
	warmups := flag.Int("warmups", 1, "unmeasured pairs per point")
	overlapText := flag.String("overlap", "", "desired per-request overlap probabilities; overrides coverage, requires workers=requests")
	coverText := flag.String("coverage", "0.01,0.05,0.1,0.25,0.5,1", "target fractions for both reads and XOR writes")
	ratioText := flag.String("read-parts", "10,9,8,7,6,5,4,3,2,1,0", "read parts out of ten")
	seed := flag.Int64("seed", 42, "base workload seed")
	retries := flag.Int("retries", 4, "optimistic retries before exclusive fallback")
	workDelay := flag.Duration("work-delay", 0, "wait inside each command attempt before validation (not CPU work)")
	jitter := flag.Bool("jitter", true, "OCC retry jitter")
	outFlag := flag.String("out", "matrix-bench-results", "new or empty output directory")
	timeout := flag.Duration("timeout", 10*time.Minute, "per-sample timeout")
	plot := flag.Bool("plot", true, "generate PNG/SVG using python3 and matplotlib")
	flag.Parse()
	if *sample != "" {
		var c sampleConfig
		if err := json.Unmarshal([]byte(*sample), &c); err != nil {
			return err
		}
		m, err := measure(c)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(m)
	}
	if *workDelay < 0 {
		return fmt.Errorf("work-delay must be non-negative")
	}
	if *n < 2 || *n > 256 || *requests < 10 || *requests%10 != 0 || *requests > 100000 || *workers < 1 || *workers > 256 || *repeats < 1 || *warmups < 0 || *retries < 0 || *timeout <= 0 {
		return fmt.Errorf("require n 2–256, requests 10–100000 divisible by 10, workers 1–256, repeats >=1, warmups/retries >=0, timeout >0")
	}
	targets := (*n) * (*n)
	var covers []float64
	seen := map[int]bool{}
	for _, v := range strings.Split(*coverText, ",") {
		f, e := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if e != nil || math.IsNaN(f) || f <= 0 || f > 1 {
			return fmt.Errorf("coverage must be in (0,1]")
		}
		k := max(1, int(math.Round(f*float64(targets))))
		if seen[k] {
			return fmt.Errorf("coverage values round to duplicate target count %d; use larger n or fewer levels", k)
		}
		seen[k] = true
		covers = append(covers, f)
	}
	var requestedOverlap, actualOverlap []float64
	if *overlapText != "" {
		if *workers != *requests {
			return fmt.Errorf("overlap mode requires workers=requests (all requests launched together)")
		}
		covers = nil
		seen = map[int]bool{}
		for _, v := range strings.Split(*overlapText, ",") {
			p, e := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if e != nil || math.IsNaN(p) || p < 0 || p > 1 {
				return fmt.Errorf("overlap must be in [0,1]")
			}
			k := nearestOverlapSize(targets, *workers, p)
			if seen[k] {
				return fmt.Errorf("overlap values map to the same %d targets; use fewer levels or larger n", k)
			}
			seen[k] = true
			covers = append(covers, float64(k)/float64(targets))
			requestedOverlap = append(requestedOverlap, p)
			actualOverlap = append(actualOverlap, overlapProbability(targets, k, *workers))
		}
	}
	var ratios []int
	seen = map[int]bool{}
	for _, v := range strings.Split(*ratioText, ",") {
		r, e := strconv.Atoi(strings.TrimSpace(v))
		if e != nil || r < 0 || r > 10 || seen[r] {
			return fmt.Errorf("read-parts requires unique integers 0–10")
		}
		seen[r] = true
		ratios = append(ratios, r)
	}
	out, err := filepath.Abs(*outFlag)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(out, 0755); err != nil {
		return err
	}
	outputLock := flock.New(out + ".lock")
	locked, err := outputLock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("benchmark output is in use: %s", out)
	}
	defer outputLock.Close()
	entries, err := os.ReadDir(out)
	if err != nil {
		return err
	}
	if len(entries) != 0 && !*resume {
		return fmt.Errorf("output directory must be empty (or use -resume): %s", out)
	}
	meta := map[string]any{"created": time.Now().Format(time.RFC3339), "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "cpu_count": runtime.NumCPU(), "gomaxprocs": runtime.GOMAXPROCS(0), "n": *n, "requests": *requests, "workers": *workers, "repeats": *repeats, "warmups": *warmups, "coverage": covers, "read_parts": ratios, "seed": *seed, "max_retries": *retries, "jitter": *jitter, "work_delay": workDelay.String(), "baseline": "same OCC Manager with one worker", "statistic": "median sequential seconds / median parallel seconds", "target_layout": "file: one file per cell; memory: atomic cell array", "journal": "disk for BOTH backends", "cache": "all targets read before timing; no cold-cache claim", "timing": "worker dispatch through all Run returns; excludes setup, verification, final cleanup wait", "output": out}
	if *overlapText != "" {
		meta["requested_overlap"] = requestedOverlap
		meta["actual_overlap"] = actualOverlap
		meta["heatmap_metric"] = "speedup"
	}
	saved := make(map[string]measurement)
	if *resume {
		saved, err = loadResume(out, meta)
		if err != nil {
			return err
		}
		fmt.Printf("Resuming with %d completed measurements\n", len(saved))
	} else {
		b, _ := json.MarshalIndent(meta, "", "  ")
		if err = os.WriteFile(filepath.Join(out, "metadata.json"), append(b, '\n'), 0644); err != nil {
			return err
		}
	}
	rawFile, err := os.OpenFile(filepath.Join(out, "raw.csv"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer rawFile.Close()
	raw := csv.NewWriter(rawFile)
	if !*resume {
		raw.Write(strings.Split(rawHeader, ","))
		raw.Flush()
	}
	var rows []row
	for _, backend := range []string{"file", "memory"} {
		for _, coverage := range covers {
			for _, readParts := range ratios {
				c := sampleConfig{Backend: backend, N: *n, Requests: *requests, Workers: *workers, ReadParts: readParts, Coverage: coverage, MaxRetries: *retries, Jitter: *jitter, WorkDelay: *workDelay}
				var serials, parallels, speeds, extras []float64
				cells := 0
				for rep := -*warmups; rep < *repeats; rep++ {
					c.Seed = *seed + int64(rep)
					order := []int{1, *workers}
					if (rep+*warmups)%2 == 1 {
						order = []int{*workers, 1}
					}
					var sm, pm measurement
					for index, w := range order {
						c.Workers = w
						isSerial := index == 0
						if (rep+*warmups)%2 == 1 {
							isSerial = !isSerial
						}
						mode := "occ"
						if isSerial {
							mode = "sequential"
						}
						key := sampleKey(backend, readParts, coverage, rep, mode)
						m, cached := saved[key]
						if !cached {
							m, err = child(c, out, *timeout)
							if err != nil {
								return fmt.Errorf("%s: %w", key, err)
							}
						}
						if isSerial {
							sm = m
						} else {
							pm = m
						}
						cells = m.Cells
						if rep >= 0 && !cached {
							raw.Write([]string{backend, strconv.Itoa(readParts), number(coverage), number(float64(cells) / float64(targets)), strconv.Itoa(cells), strconv.Itoa(rep), strconv.FormatInt(c.Seed, 10), mode, strconv.Itoa(w), number(m.Seconds), strconv.FormatInt(m.Attempts, 10), strconv.FormatInt(m.ExtraAttempts, 10), number(m.CleanupSeconds), strconv.Itoa(index)})
							raw.Flush()
							if err := rawFile.Sync(); err != nil {
								return err
							}
							if err := raw.Error(); err != nil {
								return err
							}
						}
					}
					if rep >= 0 {
						serials = append(serials, sm.Seconds)
						parallels = append(parallels, pm.Seconds)
						speeds = append(speeds, sm.Seconds/pm.Seconds)
						extras = append(extras, float64(pm.ExtraAttempts))
					}
				}
				r := row{backend, readParts, coverage, cells, quantile(serials, .5), quantile(parallels, .5), quantile(serials, .5) / quantile(parallels, .5), quantile(speeds, .25), quantile(speeds, .75), quantile(extras, .5)}
				rows = append(rows, r)
				fmt.Printf("%-6s R:W=%2d:%-2d targets=%d/%d speedup=%6.2fx serial=%7.3fms occ=%7.3fms extra=%g\n", backend, readParts, 10-readParts, cells, targets, r.Speedup, r.Serial*1000, r.Parallel*1000, r.Extra)
			}
		}
	}
	summary, err := os.Create(filepath.Join(out, "summary.csv"))
	if err != nil {
		return err
	}
	w := csv.NewWriter(summary)
	w.Write([]string{"backend", "read_parts", "coverage", "actual_coverage", "cells", "sequential_median_seconds", "occ_median_seconds", "speedup_ratio_of_medians", "paired_speedup_q25", "paired_speedup_q75", "extra_attempts_median"})
	for _, r := range rows {
		w.Write([]string{r.Backend, strconv.Itoa(r.ReadParts), number(r.Coverage), number(float64(r.Cells) / float64(targets)), strconv.Itoa(r.Cells), number(r.Serial), number(r.Parallel), number(r.Speedup), number(r.Low), number(r.High), number(r.Extra)})
	}
	w.Flush()
	err = w.Error()
	closeErr := summary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.WriteFile(filepath.Join(out, "plot.py"), []byte(plotScript), 0644); err != nil {
		return err
	}
	if *plot {
		cmd := exec.Command("python3", filepath.Join(out, "plot.py"), out)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = append(os.Environ(), "MPLCONFIGDIR="+filepath.Join(out, ".mpl-cache"))
		if err = cmd.Run(); err != nil {
			return fmt.Errorf("measurements saved; plotting requires python3 and matplotlib: %w", err)
		}
	}
	fmt.Println("Results:", out)
	return nil
}
