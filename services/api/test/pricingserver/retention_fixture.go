package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
	"github.com/jackc/pgx/v5/pgxpool"
)

// List, Delete and Exist make the fixture bucket usable by the retention job and the restore checks.
func (f *fixtureObjects) List(_ context.Context, prefix string) ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var keys []string
	for k := range f.values {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (f *fixtureObjects) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.values, key)
	return nil
}

func (f *fixtureObjects) Exists(_ context.Context, key string) (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.values[key]
	return ok, nil
}

// fixtureLedger is the in-memory backup ledger of the disposable browser server: write-once records,
// readable like the restorer identity sees them.
type fixtureLedger struct {
	mu      sync.Mutex
	records map[string][]byte
}

func (l *fixtureLedger) Exists(_ context.Context, key string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.records[key]
	return ok, nil
}

func (l *fixtureLedger) Put(_ context.Context, key string, body io.ReadSeeker, _ int64) error {
	var b bytes.Buffer
	if _, err := b.ReadFrom(body); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.records == nil {
		l.records = map[string][]byte{}
	}
	if _, ok := l.records[key]; !ok {
		l.records[key] = b.Bytes()
	}
	return nil
}

func (l *fixtureLedger) List(_ context.Context, prefix string) ([]backup.Object, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []backup.Object
	for k, v := range l.records {
		if strings.HasPrefix(k, prefix) {
			out = append(out, backup.Object{Key: k, Size: int64(len(v))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (l *fixtureLedger) GetCurrent(_ context.Context, key string) ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.records[key], nil
}

// restoreFixture simulates restoring a database dump taken before a takedown: optionally it clears
// every copy block, then it runs the same ledger replay, pin resolution and retention job a real
// restore uses. The report carries counts only.
func restoreFixture(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool, s *sharing.Service, ledger backup.BlockStore, objects *fixtureObjects) {
	var in struct {
		Rewind bool `json:"rewind_blocks"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&in) != nil {
		http.Error(w, "invalid restore request", http.StatusBadRequest)
		return
	}
	ctx, actor := r.Context(), accounts.LearnerID(r.Context())
	if in.Rewind {
		if _, err := pool.Exec(ctx, `UPDATE course_copies SET blocked_at=NULL,blocked_action_id=NULL`); err != nil {
			http.Error(w, "restore unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	reblocked, err := backup.ReplayModerationBlocks(ctx, pool, ledger)
	if err != nil {
		http.Error(w, "restore unavailable", http.StatusServiceUnavailable)
		return
	}
	unresolved := 0
	if backup.PinResolution(objects.Exists).Run(ctx, pool) != nil {
		unresolved = 1
	}
	var pins int
	if err = pool.QueryRow(ctx, `SELECT count(DISTINCT c.id) FROM courses c JOIN course_copies cc ON cc.copy_course_id=c.id JOIN course_units u ON u.course_id=c.id
JOIN course_lesson_stubs st ON st.unit_id=u.id JOIN lesson_manifests m ON m.lesson_id=st.id WHERE c.learner_id=$1 AND c.deleted_at IS NULL AND starts_with(m.manifest_key,'lessons/published/')`, actor).Scan(&pins); err != nil {
		http.Error(w, "restore unavailable", http.StatusServiceUnavailable)
		return
	}
	swept, err := s.SweepAssets(ctx, time.Now())
	if err != nil {
		http.Error(w, "restore unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"pins": pins, "unresolved_pins": unresolved, "reblocked": reblocked, "released": swept.Released, "purged": swept.Purged})
}
