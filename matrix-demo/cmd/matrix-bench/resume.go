package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func sampleKey(backend string, parts int, coverage float64, rep int, mode string) string {
	return fmt.Sprintf("%s/%d/%s/%d/%s", backend, parts, number(coverage), rep, mode)
}

func loadResume(out string, meta map[string]any) (map[string]measurement, error) {
	b, err := os.ReadFile(filepath.Join(out, "metadata.json"))
	if err != nil {
		return nil, err
	}
	var old map[string]any
	if err = json.Unmarshal(b, &old); err != nil {
		return nil, err
	}
	for _, key := range []string{"requested_overlap", "actual_overlap", "heatmap_metric", "n", "requests", "workers", "repeats", "warmups", "coverage", "read_parts", "seed", "max_retries", "jitter", "work_delay", "go", "os", "arch", "gomaxprocs"} {
		a, _ := json.Marshal(old[key])
		b, _ := json.Marshal(meta[key])
		if !bytes.Equal(a, b) {
			return nil, fmt.Errorf("resume parameter mismatch: %s", key)
		}
	}
	b, err = os.ReadFile(filepath.Join(out, "raw.csv"))
	if err != nil {
		return nil, err
	}
	// Only complete, newline-terminated records are checkpoints.
	end := bytes.LastIndexByte(b, '\n') + 1
	rows, err := csv.NewReader(bytes.NewReader(b[:end])).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || strings.Join(rows[0], ",") != rawHeader {
		return nil, fmt.Errorf("invalid raw CSV header")
	}
	saved := make(map[string]measurement)
	for i, r := range rows[1:] {
		if len(r) != 14 {
			return nil, fmt.Errorf("invalid raw row %d", i+2)
		}
		vals := make([]float64, 14)
		for _, j := range []int{1, 2, 3, 4, 5, 6, 8, 9, 10, 11, 12, 13} {
			v, e := strconv.ParseFloat(r[j], 64)
			if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("invalid raw row %d column %d", i+2, j)
			}
			vals[j] = v
		}
		if vals[9] <= 0 || vals[4] < 1 || (r[7] != "occ" && r[7] != "sequential") {
			return nil, fmt.Errorf("invalid measurement row %d", i+2)
		}
		key := sampleKey(r[0], int(vals[1]), vals[2], int(vals[5]), r[7])
		if _, ok := saved[key]; ok {
			return nil, fmt.Errorf("duplicate measurement %s", key)
		}
		saved[key] = measurement{Seconds: vals[9], CleanupSeconds: vals[12], Attempts: int64(vals[10]), ExtraAttempts: int64(vals[11]), Cells: int(vals[4])}
	}
	if end < len(b) {
		if err = os.Truncate(filepath.Join(out, "raw.csv"), int64(end)); err != nil {
			return nil, err
		}
	}
	return saved, nil
}

const rawHeader = "backend,read_parts,coverage,actual_coverage,cells,repeat,seed,mode,workers,seconds,attempts,extra_attempts,cleanup_wait_seconds,order"
