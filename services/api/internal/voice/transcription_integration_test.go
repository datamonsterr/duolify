//go:build integration

package voice_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/cache"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/voice"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// marker is planted inside the recording. It must exist only in the Engine request.
const marker = "AUDIO-MARKER-7f3a9c1e"

type engineDouble struct {
	mu    sync.Mutex
	calls int
	heard []byte
	err   error
}

func (e *engineDouble) Transcribe(_ context.Context, r *enginev1.TranscribeRequest) (*enginev1.TranscribeResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	e.heard = append([]byte(nil), r.Audio...)
	if e.err != nil {
		return nil, e.err
	}
	return &enginev1.TranscribeResponse{Text: "what is a closure", Language: "en", DurationMs: 4000, Model: "qwen/qwen3-asr-0.6b", CostNanos: 14_000}, nil
}
func (e *engineDouble) count() int { e.mu.Lock(); defer e.mu.Unlock(); return e.calls }

// switchablePricing serves the real catalogue, or the same version without a transcription rate.
type switchablePricing struct {
	cat   *pricing.Catalogue
	mu    sync.Mutex
	noVox bool
}

func (p *switchablePricing) Config() pricing.Config {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.cat.Config()
	if p.noVox {
		c.Transcribe = nil
	}
	return c
}

// recording is a small WebM stream that carries the marker as a block payload.
func recording(ms int) []byte {
	el := func(id []byte, payload []byte) []byte {
		size := []byte{0x80 | byte(len(payload))}
		if len(payload) >= 127 {
			size = []byte{0x40 | byte(len(payload)>>8), byte(len(payload))}
		}
		return append(append(append([]byte{}, id...), size...), payload...)
	}
	b := el([]byte{0x1A, 0x45, 0xDF, 0xA3}, el([]byte{0x42, 0x82}, []byte("webm")))
	b = append(b, 0x18, 0x53, 0x80, 0x67, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
	b = append(b, 0x1F, 0x43, 0xB6, 0x75, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
	b = append(b, el([]byte{0xE7}, []byte{0})...)
	for at := 0; at <= ms && at < 5000; at += 100 {
		block := append([]byte{0x81}, binary.BigEndian.AppendUint16(nil, uint16(at))...)
		block = append(block, 0x80)
		block = append(block, marker...)
		b = append(b, el([]byte{0xA3}, block)...)
	}
	return b
}

func TestTranscriptionEndpointIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	pg, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("voice_it"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, pg)
	pgURL, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, pgURL); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, pgURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = worker.Migrate(ctx, db.Pool, slog.Default()); err != nil {
		t.Fatal(err)
	}
	redisBox, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{Image: "redis:7-alpine", ExposedPorts: []string{"6379/tcp"}, WaitingFor: wait.ForLog("Ready to accept connections")}, Started: true})
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, redisBox)
	endpoint, err := redisBox.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	rdb, err := cache.New("redis://" + endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rdb.Close() }()

	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	if cat.Config().Transcribe == nil {
		t.Fatalf("the active pricing version %s has no transcription rate", cat.Config().Version)
	}
	prices := &switchablePricing{cat: cat}
	mail := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key-32-chars-long-minimum!", 2), mail)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	eng := &engineDouble{}
	events := &capture{}
	svc := voice.New(voice.Options{Engine: eng, Pricing: prices, Lessons: voice.PoolLessons{Pool: db.Pool}, Counters: voice.RedisCounters{Client: rdb}, Events: events, Logger: logger})
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Voice: svc, Postgres: db.Ping}, logger))
	defer server.Close()

	login := func(email string) (string, string) {
		post := func(path string, body map[string]any) *http.Response {
			raw, _ := json.Marshal(body)
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+path, bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			res, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			return res
		}
		if res := post("/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}); res.StatusCode != 202 {
			t.Fatalf("code: %d", res.StatusCode)
		}
		res := post("/v1/auth/verify", map[string]any{"email": email, "code": mail.Code(email), "age_confirmed": true})
		defer func() { _ = res.Body.Close() }()
		var tokens accounts.Tokens
		_ = json.NewDecoder(res.Body).Decode(&tokens)
		var id string
		_ = db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&id)
		return tokens.AccessToken, id
	}
	tokenA, learnerA := login("voice-a@example.test")
	tokenB, _ := login("voice-b@example.test")

	var quote, creation, course string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) VALUES($1::uuid,'ref',$2,'{}',now(),now()+interval '10 minutes') RETURNING id::text`, learnerA, cat.Config().Version).Scan(&quote))
	must(db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1::uuid,gen_random_uuid(),1,$2::uuid,'{}','enrolled') RETURNING id::text`, learnerA, quote).Scan(&creation))
	must(db.Pool.QueryRow(ctx, `INSERT INTO courses(learner_id,creation_id,title) VALUES($1::uuid,$2::uuid,'Astronomy') RETURNING id::text`, learnerA, creation).Scan(&course))
	unit, lesson := uuid.NewString(), uuid.NewString()
	_, err = db.Pool.Exec(ctx, `INSERT INTO course_units(id,learner_id,course_id,authored_id,position,title) VALUES($1,$2,$3,'u1',0,'Sky')`, unit, learnerA, course)
	must(err)
	_, err = db.Pool.Exec(ctx, `INSERT INTO course_lesson_stubs(id,learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1,$2,$3,'l1',0,'Stars','Goal','current')`, lesson, learnerA, unit)
	must(err)

	post := func(token string, audio []byte, fields map[string]string) (int, map[string]any, http.Header) {
		t.Helper()
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		for k, v := range fields {
			must(w.WriteField(k, v))
		}
		part, _ := w.CreateFormFile("audio", "r.webm")
		_, _ = part.Write(audio)
		must(w.Close())
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/voice/transcriptions", &body)
		req.Header.Set("Content-Type", w.FormDataContentType())
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := server.Client().Do(req)
		must(err)
		defer func() { _ = res.Body.Close() }()
		raw, _ := io.ReadAll(res.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return res.StatusCode, out, res.Header
	}
	ok := map[string]string{"purpose": "tutor", "lesson_id": lesson, "duration_ms": "4000"}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for a, b := range ok {
			m[a] = b
		}
		m[k] = v
		return m
	}
	rowCount := func(table string) int {
		var n int
		must(db.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n))
		return n
	}
	spendTables := []string{"credit_entries", "credit_jobs", "credit_usage", "credit_allocations", "credit_outbox", "pricing_quotes"}
	before := map[string]int{}
	for _, tb := range spendTables {
		before[tb] = rowCount(tb)
	}
	tempBefore := listTemp(t)

	t.Run("authentication and entitlement", func(t *testing.T) {
		if code, _, _ := post("", recording(4000), ok); code != http.StatusUnauthorized {
			t.Fatalf("anonymous: %d", code)
		}
		if code, body, _ := post(tokenB, recording(4000), ok); code != http.StatusForbidden || body["error"] != "voice_not_entitled" {
			t.Fatalf("another Learner's Lesson: %d %v", code, body)
		}
		if code, body, _ := post(tokenA, recording(4000), with("lesson_id", uuid.NewString())); code != http.StatusForbidden || body["error"] != "voice_not_entitled" {
			t.Fatalf("unknown Lesson: %d %v", code, body)
		}
		if eng.count() != 0 {
			t.Fatal("the Engine was called for a rejected request")
		}
	})

	t.Run("limits reject before any Engine call", func(t *testing.T) {
		for _, c := range []struct {
			name   string
			audio  []byte
			fields map[string]string
			status int
			code   string
		}{
			{"too large", append(recording(4000), make([]byte, voice.MaxAudioBytes)...), ok, 413, "voice_too_large"},
			{"declared 45 s", recording(4000), with("duration_ms", "45000"), 400, "voice_too_long"},
			{"wav", append([]byte("RIFF\x00\x00\x00\x00WAVE"), make([]byte, 40)...), ok, 415, "voice_unsupported_container"},
		} {
			code, body, _ := post(tokenA, c.audio, c.fields)
			if code != c.status || body["error"] != c.code {
				t.Fatalf("%s: %d %v", c.name, code, body)
			}
		}
		if eng.count() != 0 {
			t.Fatalf("the Engine was called %d times for rejected requests", eng.count())
		}
	})

	t.Run("a transcription without voice pricing is unavailable", func(t *testing.T) {
		prices.mu.Lock()
		prices.noVox = true
		prices.mu.Unlock()
		code, body, _ := post(tokenA, recording(4000), ok)
		prices.mu.Lock()
		prices.noVox = false
		prices.mu.Unlock()
		if code != http.StatusServiceUnavailable || body["error"] != "voice_unavailable" || eng.count() != 0 {
			t.Fatalf("%d %v engine=%d", code, body, eng.count())
		}
	})

	t.Run("an entitled Learner transcribes and nothing is retained", func(t *testing.T) {
		code, body, _ := post(tokenA, recording(4000), ok)
		if code != 200 || body["text"] != "what is a closure" || body["language"] != "en" || body["served_model"] != "qwen/qwen3-asr-0.6b" || body["duration_ms"] != float64(4000) {
			t.Fatalf("%d %v", code, body)
		}
		if eng.count() != 1 || !bytes.Contains(eng.heard, []byte(marker)) {
			t.Fatalf("the Engine did not receive the recording (calls %d)", eng.count())
		}
		assertNothingRetained(t, ctx, db, rdb, &logs, events, tempBefore)
	})

	t.Run("a failed transcription retains nothing either", func(t *testing.T) {
		eng.mu.Lock()
		eng.err = fmt.Errorf("provider down: %s", "x")
		eng.mu.Unlock()
		code, body, _ := post(tokenA, recording(4000), ok)
		eng.mu.Lock()
		eng.err = nil
		eng.mu.Unlock()
		if code != http.StatusBadGateway || body["error"] != "voice_failed" {
			t.Fatalf("%d %v", code, body)
		}
		assertNothingRetained(t, ctx, db, rdb, &logs, events, tempBefore)
	})

	t.Run("a transcript that is never sent spends no Credits", func(t *testing.T) {
		for _, tb := range spendTables {
			if got := rowCount(tb); got != before[tb] {
				t.Fatalf("%s changed from %d to %d rows", tb, before[tb], got)
			}
		}
	})

	t.Run("daily cap counts, expires and names its reset", func(t *testing.T) {
		day := "voice:day:" + learnerA + ":" + time.Now().UTC().Format("20060102")
		used, err := rdb.Get(ctx, day).Int()
		must(err)
		// The two earlier successes ran; the failed one gave its slot back.
		if used != 1 {
			t.Fatalf("daily counter %d, want 1", used)
		}
		if ttl := rdb.TTL(ctx, day).Val(); ttl <= 0 || ttl > 25*time.Hour {
			t.Fatalf("daily key expiry %v", ttl)
		}
		must(rdb.Set(ctx, day, 60, 2*time.Hour).Err())
		must(rdb.Del(ctx, "voice:rate:"+learnerA).Err())
		calls := eng.count()
		code, body, _ := post(tokenA, recording(4000), ok)
		if code != http.StatusTooManyRequests || body["error"] != "voice_daily_cap" {
			t.Fatalf("%d %v", code, body)
		}
		resets, perr := time.Parse(time.RFC3339, fmt.Sprint(body["resets_at"]))
		now := time.Now().UTC()
		if perr != nil || !resets.After(now) || resets.Sub(now) > 24*time.Hour+time.Minute || resets.Hour() != 0 || resets.Minute() != 0 {
			t.Fatalf("resets_at %v (%v)", body["resets_at"], perr)
		}
		if eng.count() != calls {
			t.Fatal("the Engine was called past the daily cap")
		}
		if got, _ := rdb.Get(ctx, day).Int(); got != 60 {
			t.Fatalf("a refused request moved the counter to %d", got)
		}
	})

	t.Run("burst rate limit has an expiry and Retry-After", func(t *testing.T) {
		must(rdb.Set(ctx, "voice:day:"+learnerA+":"+time.Now().UTC().Format("20060102"), 0, 2*time.Hour).Err())
		must(rdb.Del(ctx, "voice:rate:"+learnerA).Err())
		var last int
		var header http.Header
		for i := 0; i < voice.DefaultRatePerMinute+1; i++ {
			last, _, header = post(tokenA, recording(2000), with("duration_ms", "2000"))
		}
		if last != http.StatusTooManyRequests || header.Get("Retry-After") == "" {
			t.Fatalf("%d %v", last, header)
		}
		if ttl := rdb.TTL(ctx, "voice:rate:"+learnerA).Val(); ttl <= 0 || ttl > time.Minute {
			t.Fatalf("rate key expiry %v", ttl)
		}
		keys := rdb.Keys(ctx, "*").Val()
		for _, k := range keys {
			if rdb.TTL(ctx, k).Val() < 0 {
				t.Fatalf("key %s has no expiry", k)
			}
		}
	})
}

type capture struct {
	mu  sync.Mutex
	raw []string
}

func (c *capture) Capture(_ string, name string, props map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, _ := json.Marshal(map[string]any{"event": name, "props": props})
	c.raw = append(c.raw, string(b))
	return nil
}
func (c *capture) Close() error { return nil }

func listTemp(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// assertNothingRetained looks for the marker, and for the transcript, everywhere a leak could land:
// every row of every Postgres table (which holds the job queue and the outbox), every Redis key and
// value, the log output, the analytics events and the temp directory.
func assertNothingRetained(t *testing.T, ctx context.Context, db *store.Store, rdb *redis.Client, logs *bytes.Buffer, events *capture, tempBefore []string) {
	t.Helper()
	rows, err := db.Pool.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	for _, forbidden := range []string{marker, "what is a closure"} {
		for _, tb := range tables {
			var n int
			// Text of the whole row, so JSON, text and bytea columns are all covered.
			q := fmt.Sprintf(`SELECT count(*) FROM %q x WHERE x::text ILIKE $1 OR encode(convert_to(x::text,'UTF8'),'hex') LIKE $2`, tb)
			hexPattern := "%" + fmt.Sprintf("%x", forbidden) + "%"
			if err := db.Pool.QueryRow(ctx, q, "%"+forbidden+"%", hexPattern).Scan(&n); err != nil {
				t.Fatalf("scan %s: %v", tb, err)
			}
			if n != 0 {
				t.Fatalf("%q found in Postgres table %s", forbidden, tb)
			}
		}
		for _, k := range rdb.Keys(ctx, "*").Val() {
			if strings.Contains(k, forbidden) {
				t.Fatalf("%q in Redis key %s", forbidden, k)
			}
			if v, err := rdb.Get(ctx, k).Result(); err == nil && strings.Contains(v, forbidden) {
				t.Fatalf("%q in Redis value of %s", forbidden, k)
			}
		}
		if strings.Contains(logs.String(), forbidden) {
			t.Fatalf("%q in logs", forbidden)
		}
		events.mu.Lock()
		for _, e := range events.raw {
			if strings.Contains(e, forbidden) {
				t.Fatalf("%q in analytics event %s", forbidden, e)
			}
		}
		events.mu.Unlock()
	}
	after := listTemp(t)
	seen := map[string]bool{}
	for _, n := range tempBefore {
		seen[n] = true
	}
	for _, n := range after {
		if !seen[n] && !strings.HasPrefix(n, "testcontainers") && !strings.HasPrefix(n, "go-build") {
			t.Fatalf("new temp entry %s", n)
		}
	}
}
