package main

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mbb200291/go-occ-z-usecase/matrix-demo/internal/matrixdemo"
)

type sampleConfig struct {
	Backend                         string
	N, Requests, Workers, ReadParts int
	Coverage                        float64
	Seed                            int64
	MaxRetries                      int
	Jitter                          bool
	WorkDelay                       time.Duration
}

func plan(c sampleConfig) ([]matrixdemo.Job, []byte, int) {
	total := c.N * c.N
	k := max(1, int(math.Round(float64(total)*c.Coverage)))
	reads := c.Requests * c.ReadParts / 10
	rng := rand.New(rand.NewSource(c.Seed))
	jobs := make([]matrixdemo.Job, c.Requests)
	expected := make([]byte, total)
	for i := range jobs {
		kind := "xor"
		if i < reads {
			kind = "read"
		}
		cells := make([]matrixdemo.Cell, k)
		for j, id := range rng.Perm(total)[:k] {
			bit := byte(0)
			if kind == "xor" {
				bit = byte(rng.Intn(2))
				expected[id] ^= bit
			}
			cells[j] = matrixdemo.Cell{Row: id / c.N, Col: id % c.N, Bit: bit}
		}
		jobs[i] = matrixdemo.Job{ID: i, Kind: kind, Cells: cells}
	}
	rng.Shuffle(len(jobs), func(i, j int) { jobs[i], jobs[j] = jobs[j], jobs[i] })
	return jobs, expected, k
}

type storage struct {
	n      int
	dir    string
	memory []atomic.Uint32
}

func newStorage(backend string, n int) (*storage, error) {
	s := &storage{n: n}
	if backend == "memory" {
		s.memory = make([]atomic.Uint32, n*n)
		return s, nil
	}
	if backend != "file" {
		return nil, fmt.Errorf("unknown backend %q", backend)
	}
	s.dir = "targets"
	if err := os.Mkdir(s.dir, 0700); err != nil {
		return nil, err
	}
	for i := 0; i < n*n; i++ {
		if err := os.WriteFile(s.path(i), []byte{'0'}, 0600); err != nil {
			return nil, err
		}
	}
	return s, nil
}
func (s *storage) path(i int) string { return filepath.Join(s.dir, strconv.Itoa(i)) }
func (s *storage) index(key string) (int, error) {
	a, b, ok := strings.Cut(key, ",")
	r, e1 := strconv.Atoi(a)
	c, e2 := strconv.Atoi(b)
	if !ok || e1 != nil || e2 != nil || r < 0 || c < 0 || r >= s.n || c >= s.n {
		return 0, fmt.Errorf("invalid target %q", key)
	}
	return r*s.n + c, nil
}
func (s *storage) Read(key string) (any, error) {
	i, err := s.index(key)
	if err != nil {
		return nil, err
	}
	if s.memory != nil {
		return byte(s.memory[i].Load()), nil
	}
	b, err := os.ReadFile(s.path(i))
	if err != nil {
		return nil, err
	}
	return s.UnSerialize(b)
}
func (s *storage) Write(key string, value any) error {
	i, err := s.index(key)
	if err != nil {
		return err
	}
	b, err := s.Serialize(value)
	if err != nil {
		return err
	}
	if s.memory != nil {
		s.memory[i].Store(uint32(b[0] - '0'))
		return nil
	}
	f, err := os.OpenFile(s.path(i), os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	written, err := f.WriteAt(b, 0)
	if err != nil {
		return err
	}
	if written != 1 {
		return io.ErrShortWrite
	}
	return f.Sync()
}
func (*storage) Serialize(value any) ([]byte, error) {
	v, ok := value.(byte)
	if !ok || v > 1 {
		return nil, fmt.Errorf("invalid bit %v", value)
	}
	return []byte{'0' + v}, nil
}
func (*storage) UnSerialize(b []byte) (any, error) {
	if len(b) != 1 || (b[0] != '0' && b[0] != '1') {
		return nil, fmt.Errorf("invalid bit encoding")
	}
	return b[0] - '0', nil
}
