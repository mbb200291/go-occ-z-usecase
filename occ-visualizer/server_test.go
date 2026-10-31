package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mbb200291/go-occ-z/occ"
)

// Both first reads capture the old value before either transaction can proceed.
// This creates a conflict without relying on scheduler timing or fixed sleeps.
type gatedAdapter struct {
	*diskAdapter
	reads atomic.Int32
	gate  chan struct{}
}

func (a *gatedAdapter) Read(target string) (any, error) {
	value, err := a.diskAdapter.Read(target)
	n := a.reads.Add(1)
	if n == 2 {
		close(a.gate)
	}
	if n <= 2 {
		<-a.gate
	}
	return value, err
}
func TestLiveOCC(t *testing.T) {
	t.Chdir(t.TempDir())
	f, err := os.Create("counters.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = f.Truncate(targetCount * 8); err != nil {
		t.Fatal(err)
	}
	adapter := &diskAdapter{file: f}
	gate := &gatedAdapter{diskAdapter: adapter, gate: make(chan struct{})}
	server := &liveServer{adapter: adapter, start: time.Now()}
	mgr, err := occ.NewManager(gate, occ.Config{Observer: server.observe, Retry: &occ.RetryOptions{MaxRetries: 32, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	server.runner = mgr
	handler := server.routes(http.NotFoundHandler())
	post := func(body string) int {
		req := httptest.NewRequest("POST", "/api/transactions", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out.Code
	}
	if code := post(`{"count":33,"workMS":0,"distribution":"same","target":0}`); code != 400 {
		t.Fatalf("invalid count: %d", code)
	}
	if code := post(`{"count":2,"workMS":10,"distribution":"same","target":0}`); code != 202 {
		t.Fatalf("launch: %d", code)
	}
	wait := func() {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			server.mu.Lock()
			active := server.active
			server.mu.Unlock()
			if active == 0 {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("requests did not finish")
	}
	wait()
	value, err := adapter.Read("0")
	if err != nil || value != uint64(2) {
		t.Fatalf("final data %v, err %v", value, err)
	}
	server.mu.Lock()
	kinds := map[string]int{}
	attempts := map[int]int{}
	ongoing := map[string]bool{}
	prev := map[string]bool{}
	for _, e := range server.events {
		kinds[e.Kind]++
		if e.Attempt > attempts[e.Request] {
			attempts[e.Request] = e.Attempt
		}
		switch e.Kind {
		case "read":
			ongoing[e.ID] = true
		case "validate":
			prev[e.ID] = true
		case "conflict":
			delete(prev, e.ID)
		case "purge":
			delete(prev, e.OtherID)
		case "end":
			delete(ongoing, e.ID)
			if e.Error == "" && e.Write == 0 {
				t.Error("successful end lacks WriteTime")
			}
		}
	}
	completed, failed := server.completed, server.failed
	server.mu.Unlock()
	if kinds["conflict"] < 1 || kinds["blocked"] < 1 || kinds["purge"] < 1 || kinds["commit"] != 2 || completed != 2 || failed != 0 || len(ongoing) != 0 || len(prev) != 1 {
		t.Fatalf("unexpected trace: %v, completed=%d failed=%d ongoing=%v prev=%v", kinds, completed, failed, ongoing, prev)
	}
	if attempts[1] < 2 && attempts[2] < 2 {
		t.Fatal("expected fresh retry attempt")
	}
	// Additional commands use the same real Manager and storage.
	if code := post(`{"count":4,"workMS":0,"distribution":"spread","target":1}`); code != 202 {
		t.Fatal(code)
	}
	wait()
	if code := post(`{"count":1,"workMS":0,"distribution":"same","target":0,"readOnly":true}`); code != 202 {
		t.Fatal(code)
	}
	wait()
	req := httptest.NewRequest("GET", "/api/events?after=0", nil)
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	var result struct {
		Values    []uint64
		Completed int
		Events    []liveEvent
	}
	if err = json.Unmarshal(out.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for i, e := range result.Events {
		if i > 0 && e.Time < result.Events[i-1].Time {
			t.Fatal("event timestamps out of order")
		}
		if e.Kind == "validate" && (len(e.ReadSet) != 1 || e.ReadSet[0] != e.Target) {
			t.Fatalf("incorrect request association: %+v", e)
		}
	}
	if result.Completed != 7 || len(result.Values) != 8 || result.Values[0] != 2 || result.Values[4] != 1 {
		t.Fatalf("snapshot: %+v", result)
	}
	// A stale browser session must not combine events from another server run.
	req = httptest.NewRequest("GET", "/api/events?after=99999&session=old-server", nil)
	out = httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	if out.Code != http.StatusConflict {
		t.Fatalf("stale session status %d", out.Code)
	}
	// The browser endpoint rejects cross-origin writes.
	req = httptest.NewRequest("POST", "/api/transactions", bytes.NewBufferString(`{}`))
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	out = httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	if out.Code != 403 {
		t.Fatalf("cross-origin status %d", out.Code)
	}
	// Wait for the package's asynchronous journal cleanup before t.Chdir restores cwd.
	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, err := os.ReadDir(".occ")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("journal cleanup timed out")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestJitterSetting(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		options := demoRetry(enabled)
		if options.Jitter != enabled || options.MaxRetries != 16 || options.BaseDelay != 50*time.Millisecond || options.MaxDelay != 500*time.Millisecond {
			t.Fatal(options)
		}
	}
}
