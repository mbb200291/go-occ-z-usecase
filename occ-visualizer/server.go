package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/mbb200291/go-occ-z/occ"
)

const targetCount = 8

// Each target occupies its own eight-byte slot. Independent targets never
// replace the whole file. OCC handles logical conflicts; no adapter-wide lock.
type diskAdapter struct {
	file       *os.File
	writeDelay time.Duration
}

func targetIndex(target string) (int, error) {
	n, err := strconv.Atoi(target)
	if err != nil || n < 0 || n >= targetCount {
		return 0, fmt.Errorf("invalid target %q", target)
	}
	return n, nil
}
func (a *diskAdapter) Read(target string) (any, error) {
	n, err := targetIndex(target)
	if err != nil {
		return nil, err
	}
	var data [8]byte
	if _, err = a.file.ReadAt(data[:], int64(n*8)); err != nil {
		return nil, err
	}
	return binary.LittleEndian.Uint64(data[:]), nil
}
func (a *diskAdapter) Write(target string, value any) error {
	n, err := targetIndex(target)
	if err != nil {
		return err
	}
	data, err := a.Serialize(value)
	if err != nil {
		return err
	}
	time.Sleep(a.writeDelay) // Intentional workload latency, never synchronization.
	written, err := a.file.WriteAt(data, int64(n*8))
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return a.file.Sync()
}
func (*diskAdapter) Serialize(value any) ([]byte, error) {
	n, ok := value.(uint64)
	if !ok {
		return nil, fmt.Errorf("expected uint64, got %T", value)
	}
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, n)
	return data, nil
}
func (*diskAdapter) UnSerialize(data []byte) (any, error) {
	if len(data) != 8 {
		return nil, errors.New("invalid backup length")
	}
	return binary.LittleEndian.Uint64(data), nil
}

type increment struct {
	server           *liveServer
	request, attempt int
	lastID           string
	target           string
	delay            time.Duration
	readOnly         bool
}

func (c *increment) Execute(ctx occ.Context) (any, error) {
	c.server.bind(c)
	value, err := ctx.GetData(c.target)
	if err != nil {
		return nil, err
	}
	n, ok := value.(uint64)
	if !ok {
		return nil, errors.New("expected uint64")
	}
	time.Sleep(c.delay)
	if !c.readOnly {
		n++
		ctx.SetData(c.target, n)
	}
	return n, nil
}

type runner interface {
	Run([]occ.Command) ([]any, error)
}
type liveEvent struct {
	occ.TraceEvent
	Seq      int     `json:"seq"`
	Time     float64 `json:"time"`
	Request  int     `json:"request"`
	Attempt  int     `json:"attempt"`
	Target   string  `json:"target"`
	ReadOnly bool    `json:"readOnly"`
}
type liveServer struct {
	// Pair the synchronous read event with the next Execute entry.
	// Release before any command work or I/O, so transactions still overlap.
	registration sync.Mutex
	pending      occ.TraceEvent
	attempts     map[string]*increment

	mu                               sync.Mutex
	runner                           runner
	adapter                          *diskAdapter
	start                            time.Time
	events                           []liveEvent
	total, active, completed, failed int
}
type runInput struct {
	Count        int    `json:"count"`
	WorkMS       int    `json:"workMS"`
	Distribution string `json:"distribution"`
	Target       int    `json:"target"`
	ReadOnly     bool   `json:"readOnly"`
}

func (s *liveServer) record(request, attempt int, target string, readOnly bool, e occ.TraceEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, liveEvent{TraceEvent: e, Seq: len(s.events) + 1, Time: time.Since(s.start).Seconds(), Request: request, Attempt: attempt, Target: target, ReadOnly: readOnly})
}
func (s *liveServer) launch(w http.ResponseWriter, r *http.Request) {
	input := runInput{}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "expected one JSON object", 400)
		return
	}
	if input.Count < 1 || input.Count > 32 || input.WorkMS < 0 || input.WorkMS > 2000 || input.Target < 0 || input.Target >= targetCount || (input.Distribution != "same" && input.Distribution != "spread") {
		http.Error(w, "count 1–32, workMS 0–2000, target 0–7; distribution same/spread", 400)
		return
	}
	s.mu.Lock()
	if s.active+input.Count > 32 || s.total+input.Count > 128 {
		s.mu.Unlock()
		http.Error(w, "最多同時 32 個 request，每次伺服器執行累計 128 個；請等待或重新啟動", 429)
		return
	}
	first := s.total + 1
	s.total += input.Count
	s.active += input.Count
	s.mu.Unlock()
	gate := make(chan struct{})
	for i := 0; i < input.Count; i++ {
		target := input.Target
		if input.Distribution == "spread" {
			target = (target + i) % targetCount
		}
		go func(request int, target string) {
			<-gate
			cmd := &increment{server: s, request: request, target: target, delay: time.Duration(input.WorkMS) * time.Millisecond, readOnly: input.ReadOnly}
			_, err := s.runner.Run([]occ.Command{cmd})
			e := occ.TraceEvent{Kind: "request_done", ID: cmd.lastID}
			if err != nil {
				e.Error = err.Error()
			}
			s.record(request, cmd.attempt, target, input.ReadOnly, e)
			s.mu.Lock()
			s.active--
			if err == nil {
				s.completed++
			} else {
				s.failed++
			}
			s.mu.Unlock()
		}(first+i, strconv.Itoa(target))
	}
	close(gate)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]int{"accepted": input.Count})
}
func (s *liveServer) poll(w http.ResponseWriter, r *http.Request) {
	session := strconv.FormatInt(s.start.UnixNano(), 10)
	if previous := r.URL.Query().Get("session"); previous != "" && previous != session {
		http.Error(w, "server restarted", http.StatusConflict)
		return
	}
	after := 0
	var err error
	if raw := r.URL.Query().Get("after"); raw != "" {
		after, err = strconv.Atoi(raw)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil || after < 0 || after > len(s.events) {
		http.Error(w, "invalid event cursor", 400)
		return
	}
	// Read storage only when all Run calls have ended. Active values could be dirty.
	var values []uint64
	if s.active == 0 {
		for i := 0; i < targetCount; i++ {
			value, err := s.adapter.Read(strconv.Itoa(i))
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			values = append(values, value.(uint64))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Session   string      `json:"session"`
		Events    []liveEvent `json:"events"`
		Now       float64     `json:"now"`
		Active    int         `json:"active"`
		Completed int         `json:"completed"`
		Failed    int         `json:"failed"`
		Total     int         `json:"total"`
		Values    []uint64    `json:"values"`
	}{session, s.events[after:], time.Since(s.start).Seconds(), s.active, s.completed, s.failed, s.total, values})
}
func (s *liveServer) routes(static http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/transactions", s.launch)
	mux.HandleFunc("GET /api/events", s.poll)
	mux.Handle("GET /", static)
	return http.NewCrossOriginProtection().Handler(mux)
}

func demoRetry(jitter bool) occ.RetryOptions {
	return occ.RetryOptions{MaxRetries: 16, BaseDelay: 50 * time.Millisecond, MaxDelay: 500 * time.Millisecond, Jitter: jitter}
}

func (s *liveServer) observe(e occ.TraceEvent) {
	if e.Kind == "read" {
		s.registration.Lock()
		s.pending = e
		return
	}
	s.mu.Lock()
	cmd := s.attempts[e.ID]
	if e.Kind == "end" {
		delete(s.attempts, e.ID)
	}
	s.mu.Unlock()
	if cmd != nil {
		s.record(cmd.request, cmd.attempt, cmd.target, cmd.readOnly, e)
	}
}

func (s *liveServer) bind(cmd *increment) {
	e := s.pending
	cmd.attempt++
	cmd.lastID = e.ID
	s.mu.Lock()
	if s.attempts == nil {
		s.attempts = make(map[string]*increment)
	}
	s.attempts[e.ID] = cmd
	s.events = append(s.events, liveEvent{TraceEvent: e, Seq: len(s.events) + 1, Time: time.Since(s.start).Seconds(), Request: cmd.request, Attempt: cmd.attempt, Target: cmd.target, ReadOnly: cmd.readOnly})
	s.mu.Unlock()
	s.registration.Unlock()
}
