package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDemoChild(t *testing.T) {
	raw := os.Getenv("OCC_MATRIX_ARGS")
	if raw == "" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatal(err)
	}
	if err := run(args, os.Stdout); err != nil {
		t.Fatal(err)
	}
}

// Separate processes match the process-lifetime lock and keep cwd isolated.
func TestDemoSuite(t *testing.T) {
	var firstPlan []byte
	for _, tc := range []struct{ workers, reads, readSize int }{{1, 12, 1}, {4, 12, 1}, {8, 20, 16}, {2, 0, 4}} {
		t.Run(fmt.Sprintf("workers=%d/reads=%d/size=%d", tc.workers, tc.reads, tc.readSize), func(t *testing.T) {
			dir := t.TempDir()
			args := []string{"-dir", dir, "-n", "4", "-blocks", "8", "-block-size", "2", "-reads", fmt.Sprint(tc.reads), "-read-size", fmt.Sprint(tc.readSize), "-workers", fmt.Sprint(tc.workers), "-pause", "0", "-interval", "10ms"}
			raw, _ := json.Marshal(args)
			cmd := exec.Command(os.Args[0], "-test.run=^TestDemoChild$")
			cmd.Env = append(os.Environ(), "OCC_MATRIX_ARGS="+string(raw))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if !strings.Contains(string(out), "PASS: all 16 cells are 1") {
				t.Fatalf("no success: %s", out)
			}
			data, err := os.ReadFile(filepath.Join(dir, "matrix.csv"))
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != strings.Repeat("1,1,1,1\n", 4) {
				t.Fatal(string(data))
			}
			plan, err := os.ReadFile(filepath.Join(dir, "commands.json"))
			if err != nil {
				t.Fatal(err)
			}
			if tc.workers == 1 {
				firstPlan = plan
			}
			if tc.workers == 4 && string(plan) != string(firstPlan) {
				t.Fatal("worker count changed command set")
			}
			events, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(events), `"kind":"display-read"`) {
				t.Fatal("no read-command display")
			}
		})
	}
}
