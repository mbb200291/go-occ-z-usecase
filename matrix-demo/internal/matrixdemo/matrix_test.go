package matrixdemo

import (
	"github.com/mbb200291/go-occ-z/occ"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlansEndAtAllOnes(t *testing.T) {
	for _, n := range []int{2, 3, 8} {
		for _, seed := range []int64{1, 42, 99} {
			cfg := Config{Size: n, Blocks: 20, BlockSize: min(3, n-1), Reads: 12, ReadSize: n, Seed: seed}
			jobs, err := Generate(cfg)
			if err != nil {
				t.Fatal(err)
			}
			again, _ := Generate(cfg)
			if !reflect.DeepEqual(jobs, again) {
				t.Fatal("seed not reproducible")
			}
			parity := make([]byte, n*n)
			reads := 0
			// Reverse order independently of the generated shuffle.
			for i := len(jobs) - 1; i >= 0; i-- {
				j := jobs[i]
				if j.Kind == "read" {
					reads++
					if len(j.Cells) != cfg.ReadSize {
						t.Fatal("read size")
					}
					continue
				}
				if len(j.Cells) >= n*n {
					t.Fatal("full matrix write")
				}
				for _, p := range j.Cells {
					if p.Bit > 1 {
						t.Fatal("invalid bit")
					}
					parity[p.Row*n+p.Col] ^= p.Bit
				}
			}
			if reads != cfg.Reads {
				t.Fatal("read count")
			}
			for i, v := range parity {
				if v != 1 {
					t.Fatalf("n=%d seed=%d cell=%d value=%d", n, seed, i, v)
				}
			}
		}
	}
}
func TestCSVOffsetsAndBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matrix.csv")
	a, err := Create(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, key := range []string{"0,0", "1,2", "2,2"} {
		if err := a.Write(key, byte(1)); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "1,0,0\n0,0,1\n0,0,1\n" {
		t.Fatalf("CSV: %q", data)
	}
	v, err := a.Read("1,2")
	if err != nil || v != byte(1) {
		t.Fatalf("read=%v err=%v", v, err)
	}
	b, err := a.Serialize(v)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := a.UnSerialize(b)
	if err != nil || restored != v {
		t.Fatal("backup roundtrip")
	}
	if err := a.Write("0,0", byte(2)); err == nil {
		t.Fatal("accepted nonbinary value")
	}
	if _, err := a.Read("3,0"); err == nil {
		t.Fatal("accepted out of bounds")
	}
	if _, err := Create(path, 3); err == nil {
		t.Fatal("overwrote existing matrix")
	}
}

type workspace struct {
	occ.Context // Only command data access is exercised by this test.
	data        map[string]any
}

func (w *workspace) GetData(k string) (any, error) { return w.data[k], nil }
func (w *workspace) SetData(k string, v any)       { w.data[k] = v }
func TestCommands(t *testing.T) {
	ctx := &workspace{data: map[string]any{"0,0": byte(0), "0,1": byte(1)}}
	job := Job{Kind: "xor", Cells: []Cell{{0, 0, 1}, {0, 1, 0}}}
	cmd := &Command{Job: job}
	if cmd.IsReadOnly() || cmd.IsWriteOnly() {
		t.Fatal("xor must read and write")
	}
	if outcome, err := cmd.Execute(ctx); err != nil || outcome != nil {
		t.Fatal(err)
	}
	if ctx.data["0,0"] != byte(1) || ctx.data["0,1"] != byte(1) {
		t.Fatal(ctx.data)
	}
	read := &Command{Job: Job{Kind: "read", Cells: job.Cells}}
	if !read.IsReadOnly() {
		t.Fatal("read metadata")
	}
	outcome, err := read.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(outcome, []byte{1, 1}) {
		t.Fatal(outcome)
	}
}
