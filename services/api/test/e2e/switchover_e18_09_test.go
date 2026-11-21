package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/switchover"
	"github.com/datamonsterr/duolify/services/api/internal/switchover/switchovertest"
)

const switchoverSchema = `CREATE TABLE progress(id bigserial PRIMARY KEY, note text NOT NULL)`

const operatorToken = "operator-test-token"

// TestSwitchoverUnderLoad triggers the gate between two Postgres 18 instances while writes, reads, an open
// SSE stream and a narration-style session advisory lock holder run, and measures the pause.
func TestSwitchoverUnderLoad(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	pair := switchovertest.New(ctx, t, switchoverSchema)
	gate, err := switchover.New(switchover.Options{SourceURL: pair.SourceURL, TargetURL: pair.TargetURL, Subscription: switchovertest.Subscription, HoldMax: 15 * time.Second, SequenceMargin: 1000})
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, pair.SourceURL, gate.PoolConfig)
	if err != nil {
		t.Fatal(err)
	}
	gate.Attach(db.Pool)
	t.Cleanup(db.Close)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/progress", func(w http.ResponseWriter, r *http.Request) {
		var id int64
		if err := db.Pool.QueryRow(r.Context(), "INSERT INTO progress(note) VALUES('p') RETURNING id").Scan(&id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]int64{"id": id})
	})
	mux.HandleFunc("GET /v1/progress", func(w http.ResponseWriter, r *http.Request) {
		var n int
		if err := db.Pool.QueryRow(r.Context(), "SELECT count(*) FROM progress").Scan(&n); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	// Narration shape: one pooled connection holds a session advisory lock while nested pool queries run.
	mux.HandleFunc("POST /v1/narrate", func(w http.ResponseWriter, r *http.Request) {
		conn, err := db.Pool.Acquire(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer conn.Release()
		if _, err = conn.Exec(r.Context(), "SELECT pg_advisory_lock(hashtextextended('narration:x',0))"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() {
			_, _ = conn.Exec(context.WithoutCancel(r.Context()), "SELECT pg_advisory_unlock(hashtextextended('narration:x',0))")
		}()
		var n int
		if err = db.Pool.QueryRow(r.Context(), "SELECT count(*) FROM progress").Scan(&n); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		select {
		case <-time.After(400 * time.Millisecond):
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("GET /v1/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for i := 0; ; i++ {
			if _, err := fmt.Fprintf(w, "data: %d\n\n", i); err != nil {
				return
			}
			flusher.Flush()
			select {
			case <-time.After(25 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.Handle("/admin/v1/switchover/", gate.Handler(func(r *http.Request) (bool, error) {
		return r.Header.Get("Authorization") == "Bearer "+operatorToken, nil
	}))
	srv := httptest.NewServer(gate.Middleware(mux))
	t.Cleanup(srv.Close)

	var failures atomic.Int64
	var maxLatency, maxStreamGap atomic.Int64
	var acked sync.Map
	var requests, events atomic.Int64
	record := func(max *atomic.Int64, d time.Duration) {
		for {
			cur := max.Load()
			if int64(d) <= cur || max.CompareAndSwap(cur, int64(d)) {
				return
			}
		}
	}
	loadCtx, stopLoad := context.WithCancel(ctx)
	var wg sync.WaitGroup
	call := func(method, path string) {
		started := time.Now()
		req, _ := http.NewRequestWithContext(loadCtx, method, srv.URL+path, nil)
		resp, err := srv.Client().Do(req)
		if err != nil {
			if loadCtx.Err() == nil {
				failures.Add(1)
				t.Errorf("%s %s: %v", method, path, err)
			}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		record(&maxLatency, time.Since(started))
		requests.Add(1)
		if resp.StatusCode != http.StatusOK {
			failures.Add(1)
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("%s %s = %d %s", method, path, resp.StatusCode, body)
			return
		}
		if method == "POST" && path == "/v1/progress" {
			var out struct{ ID int64 }
			if json.NewDecoder(resp.Body).Decode(&out) == nil {
				acked.Store(out.ID, true)
			}
		}
	}
	loop := func(method, path string, every time.Duration) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for loadCtx.Err() == nil {
				call(method, path)
				time.Sleep(every)
			}
		}()
	}
	for range 6 {
		loop("POST", "/v1/progress", 5*time.Millisecond)
	}
	loop("GET", "/v1/progress", 5*time.Millisecond)
	loop("POST", "/v1/narrate", 20*time.Millisecond)
	loop("POST", "/v1/narrate", 20*time.Millisecond)
	// One SSE stream stays open for the whole run; any gap or read error is a failure.
	wg.Add(1)
	go func() {
		defer wg.Done()
		req, _ := http.NewRequestWithContext(loadCtx, "GET", srv.URL+"/v1/stream", nil)
		resp, err := srv.Client().Do(req)
		if err != nil {
			failures.Add(1)
			t.Errorf("stream: %v", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		last := time.Now()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "data:") {
				record(&maxStreamGap, time.Since(last))
				last = time.Now()
				events.Add(1)
			}
		}
		if loadCtx.Err() == nil {
			failures.Add(1)
			t.Errorf("stream ended early: %v", scanner.Err())
		}
	}()

	time.Sleep(1500 * time.Millisecond)
	trigger := func(withToken bool) (int, time.Duration) {
		req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/admin/v1/switchover/switch", nil)
		if withToken {
			req.Header.Set("Authorization", "Bearer "+operatorToken)
		}
		started := time.Now()
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode, time.Since(started)
	}
	if code, _ := trigger(false); code != http.StatusForbidden {
		t.Fatalf("trigger without operator auth = %d", code)
	}
	code, took := trigger(true)
	if code != http.StatusOK {
		t.Fatalf("switch = %d", code)
	}
	time.Sleep(1500 * time.Millisecond)
	stopLoad()
	wg.Wait()

	pause := time.Duration(maxLatency.Load())
	t.Logf("switch call %v, longest request %v, longest SSE gap %v, %d requests, %d SSE events", took, pause, time.Duration(maxStreamGap.Load()), requests.Load(), events.Load())
	if failures.Load() != 0 {
		t.Fatalf("%d request errors during switchover", failures.Load())
	}
	if pause > 15*time.Second || took > 15*time.Second {
		t.Fatalf("pause %v / switch %v exceed 15 s", pause, took)
	}
	if gap := time.Duration(maxStreamGap.Load()); gap > 2*time.Second {
		t.Fatalf("SSE stream stalled for %v", gap)
	}
	// Every acknowledged write is on the new database; none were lost to the old one.
	var missing int
	acked.Range(func(k, _ any) bool {
		var found bool
		if err := switchovertest.QueryRow(ctx, t, pair.TargetURL, "SELECT EXISTS(SELECT 1 FROM progress WHERE id=$1)", k).Scan(&found); err != nil {
			t.Fatal(err)
		}
		if !found {
			missing++
		}
		return true
	})
	if missing != 0 {
		t.Fatalf("%d acknowledged writes are missing from the new database", missing)
	}
}

// TestSwitchoverTriggerNeedsOperator drives the real router: the trigger is unreachable without a
// session, and a signed-in learner who is not an operator is refused without changing the gate.
func TestSwitchoverTriggerNeedsOperator(t *testing.T) {
	auth, token, db := signedInDatabase(t)
	gate, err := switchover.New(switchover.Options{SourceURL: "postgres://u:p@old:5432/app", TargetURL: "postgres://u:p@new:5432/app"})
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(httpapi.Dependencies{Accounts: auth, Switchover: gate.Handler(switchover.OperatorOnly(db.Pool)), Postgres: func(context.Context) error { return nil }}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	call := func(method, path, bearer string) int {
		req := httptest.NewRequestWithContext(t.Context(), method, path, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}
	for _, route := range [][2]string{{"POST", "/admin/v1/switchover/switch"}, {"POST", "/admin/v1/switchover/readonly"}, {"POST", "/admin/v1/switchover/open"}, {"GET", "/admin/v1/switchover/status"}} {
		if code := call(route[0], route[1], ""); code != http.StatusUnauthorized {
			t.Errorf("%v without a session = %d", route, code)
		}
		if code := call(route[0], route[1], token); code != http.StatusForbidden {
			t.Errorf("%v as a non-operator = %d", route, code)
		}
	}
	if gate.State() != switchover.StateOpen {
		t.Fatalf("gate state = %s", gate.State())
	}
	var learner string
	if err = db.Pool.QueryRow(t.Context(), "SELECT id::text FROM learners LIMIT 1").Scan(&learner); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(t.Context(), "INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)", learner); err != nil {
		t.Fatal(err)
	}
	if code := call("GET", "/admin/v1/switchover/status", token); code != http.StatusOK {
		t.Fatalf("operator status = %d", code)
	}
}
