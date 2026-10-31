package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"testing"
)

func TestPlanRatiosFootprintAndParity(t *testing.T) {
	for r := 0; r <= 10; r++ {
		for _, coverage := range []float64{.01, .25, 1} {
			c := sampleConfig{N: 8, Requests: 20, ReadParts: r, Coverage: coverage, Seed: 42}
			jobs, expected, k := plan(c)
			again, _, _ := plan(c)
			if !reflect.DeepEqual(jobs, again) {
				t.Fatal("seed not deterministic")
			}
			reads := 0
			actual := make([]byte, 64)
			for i := len(jobs) - 1; i >= 0; i-- {
				job := jobs[i]
				if job.Kind == "read" {
					reads++
				}
				if len(job.Cells) != k {
					t.Fatal("unequal footprints")
				}
				seen := map[int]bool{}
				for _, cell := range job.Cells {
					id := cell.Row*8 + cell.Col
					if seen[id] {
						t.Fatal("duplicate target")
					}
					seen[id] = true
					if job.Kind == "xor" {
						actual[id] ^= cell.Bit
					}
				}
			}
			if reads != 2*r || !reflect.DeepEqual(actual, expected) {
				t.Fatalf("bad workload: ratio=%d", r)
			}
		}
	}
}
func TestSampleChild(t *testing.T) {
	raw := os.Getenv("OCC_BENCH_TEST")
	if raw == "" {
		return
	}
	var c sampleConfig
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	m, err := measure(c)
	if err != nil {
		t.Fatal(err)
	}
	if m.Attempts < int64(c.Requests) || m.Seconds <= 0 {
		t.Fatalf("invalid measurement %+v", m)
	}
	if c.Workers == 1 && m.ExtraAttempts != 0 {
		t.Fatal("sequential run retried")
	}
}
func TestBothStorageModes(t *testing.T) {
	for _, backend := range []string{"file", "memory"} {
		for _, workers := range []int{1, 4} {
			for _, reads := range []int{0, 5, 10} {
				t.Run(backend+"/"+strconv.Itoa(workers)+"/"+strconv.Itoa(reads), func(t *testing.T) {
					c := sampleConfig{Backend: backend, N: 2, Requests: 10, Workers: workers, ReadParts: reads, Coverage: 1, Seed: 9, MaxRetries: 0, Jitter: true}
					raw, _ := json.Marshal(c)
					cmd := exec.Command(os.Args[0], "-test.run=^TestSampleChild$", "-test.timeout=30s")
					cmd.Dir = t.TempDir()
					cmd.Env = append(os.Environ(), "OCC_BENCH_TEST="+string(raw))
					if output, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("%v\n%s", err, output)
					}
				})
			}
		}
	}
}
