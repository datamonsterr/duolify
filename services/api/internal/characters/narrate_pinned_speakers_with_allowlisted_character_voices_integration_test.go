//go:build integration

package characters_test

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/narration"
	"github.com/datamonsterr/duolify/services/api/internal/narration/narrationtest"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	narrOwner  = "a1111111-1111-4111-8111-111111111111"
	narrLesson = "f3333333-3333-4333-8333-333333333333"
	narrCourse = "f1111111-1111-4111-8111-111111111111"
)

type events struct {
	mu   sync.Mutex
	list []map[string]any
}

func (r *events) Capture(_ string, event string, p map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := map[string]any{"event": event}
	for k, v := range p {
		e[k] = v
	}
	r.list = append(r.list, e)
	return nil
}
func (r *events) Close() error { return nil }
func (r *events) named(name string) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]any
	for _, e := range r.list {
		if e["event"] == name {
			out = append(out, e)
		}
	}
	return out
}

type plan struct{ entitled bool }

func (p *plan) FundedPlan(context.Context, string, time.Time) (*credits.FundedPlan, error) {
	if !p.entitled {
		return nil, nil
	}
	return &credits.FundedPlan{Active: true, Benefits: []string{narration.BenefitCuratedVoices}}, nil
}

type harness struct {
	f       *backuptest.Fixture
	twin    *narrationtest.Engine
	service *narration.Service
	events  *events
	plan    *plan
	objects narration.Objects
}

// setup builds the real Postgres/River/object-store stack with the engine twin behind real gRPC.
func setup(t *testing.T) *harness {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)
	f := backuptest.New(t)
	f.Seed(t)
	pool := f.DB.Pool
	h := &harness{f: f, twin: &narrationtest.Engine{}, events: &events{}, plan: &plan{entitled: true}, objects: f.ApplicationObjects()}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("step 1", err)
	}
	server := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(server, h.twin)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal("step 2", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err = pool.Exec(ctx, `UPDATE learner_profiles SET narration=true,narration_voice='default' WHERE learner_id=$1`, narrOwner); err != nil {
		t.Fatal("step 3", err)
	}
	catalogue, err := pricing.Load(ctx, f.DB.Queries)
	if err != nil {
		t.Fatal("pricing load", err)
	}
	financial := credits.NewServiceWithCatalogue(pool, nil, catalogue, h.events)
	if _, err = pool.Exec(ctx, `INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance) VALUES ($1, 50, 'purchase', 'narration-topup:' || gen_random_uuid(), '2026.04', '{"bucket": "purchase"}'::jsonb)`, narrOwner); err != nil {
		t.Fatal("step 4", err)
	}
	for {
		var id string
		if pool.QueryRow(ctx, `SELECT id::text FROM credit_jobs WHERE learner_id=$1 AND state='reserved' LIMIT 1`, narrOwner).Scan(&id) != nil {
			break
		}
		if _, err = financial.Release(ctx, narrOwner, id); err != nil {
			t.Fatal("release", id, err)
		}
	}
	h.service = narration.New(narration.Options{Pool: pool, Objects: h.objects, Speech: enginev1.NewEngineServiceClient(conn), Financial: financial, Entitlements: h.plan,
		Events: h.events, JobAttempts: 1, FixtureVoices: true, Segments: characters.NewService(pool, nil, h.events)})
	queue, err := h.service.Queue()
	if err != nil {
		t.Fatal("step 5", err)
	}
	if err = queue.Start(ctx); err != nil {
		t.Fatal("step 6", err)
	}
	t.Cleanup(func() { _ = queue.Stop(context.WithoutCancel(ctx)) })
	// The Lesson's pinned default Cast and dialogue: guide, supporter, then the guide repeats a line.
	if _, err = pool.Exec(ctx, `UPDATE generated_lessons SET manifest=jsonb_build_object('title','Phases','cast_key','f4444444-4444-4444-8444-444444444444') WHERE lesson_stub_id=$1`, narrLesson); err != nil {
		t.Fatal("step 7", err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM lesson_slides WHERE lesson_stub_id=$1`, narrLesson); err != nil {
		t.Fatal("step 8", err)
	}
	for i, slide := range []struct{ id, segments string }{
		{"sky-1", `[{"speaker_key":"guide","speaker_label":"G","text":"Look closely at the sky.","expression":"happy","start_ms":0,"duration_ms":1000},
			{"speaker_key":"supporter","speaker_label":"S","text":"Earth turns every day.","expression":"thinking","start_ms":1000,"duration_ms":1000},
			{"speaker_key":"guide","speaker_label":"G","text":"Look closely at the sky.","expression":"happy","start_ms":2000,"duration_ms":1000}]`},
		{"sky-2", `[{"speaker_key":"supporter","speaker_label":"S","text":"Earth turns every day.","expression":"idle","start_ms":0,"duration_ms":1500}]`},
		{"sky-3", `[]`},
	} {
		if _, err = pool.Exec(ctx, `INSERT INTO lesson_slides(lesson_stub_id,learner_id,position,slide_id,html,duration_ms,narration,segments) VALUES($1,$2,$3,$4,'<h1>x</h1>',3000,'',$5::jsonb)`, narrLesson, narrOwner, i, slide.id, slide.segments); err != nil {
			t.Fatal("step 9", err)
		}
	}
	return h
}

type segmentRow struct {
	Slide, Speaker, Requested, Actual, Version, Fallback, Status, Code, Hash string
	Index                                                                    int
}

func (h *harness) rows(t *testing.T) []segmentRow {
	t.Helper()
	rows, err := h.f.DB.Pool.Query(t.Context(), `SELECT segment_slide_id,segment_index,speaker_key,voice_key,actual_voice_key,voice_config_version,voice_fallback_reason,status,failure_code,content_hash
		FROM slide_narrations WHERE lesson_id=$1 AND speaker_key<>'' ORDER BY segment_slide_id,segment_index`, narrLesson)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []segmentRow
	for rows.Next() {
		var r segmentRow
		if err = rows.Scan(&r.Slide, &r.Index, &r.Speaker, &r.Requested, &r.Actual, &r.Version, &r.Fallback, &r.Status, &r.Code, &r.Hash); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func (h *harness) settle(t *testing.T, want int) []segmentRow {
	t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for {
		rows := h.rows(t)
		pending := 0
		for _, r := range rows {
			if r.Status == narration.StatusPending {
				pending++
			}
		}
		if len(rows) == want && pending == 0 {
			// Wait for the River job itself to finish so the next enqueue starts a new run.
			var open int
			if h.f.DB.Pool.QueryRow(t.Context(), `SELECT count(*) FROM river_job WHERE kind='narration_synthesize' AND state NOT IN ('completed','discarded','cancelled')`).Scan(&open) == nil && open == 0 {
				return rows
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("segments did not settle: %+v", rows)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (h *harness) spent(t *testing.T) (settled, released, audio int) {
	t.Helper()
	rows, err := h.f.DB.Pool.Query(t.Context(), `SELECT j.state, COALESCE((j.result->>'audio_spent')::int,0), q.snapshot->>'action'
		FROM credit_jobs j JOIN pricing_quotes q ON q.id=j.quote_id WHERE j.learner_id=$1 AND q.snapshot->>'action' IN ('narration','character_create')`, narrOwner)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var state, action string
		var a int
		if err = rows.Scan(&state, &a, &action); err != nil {
			t.Fatal(err)
		}
		if action != "narration" {
			t.Fatalf("a %s charge was made by narration", action)
		}
		switch state {
		case "settled":
			settled++
			audio += a
		case "released":
			released++
		default:
			t.Fatalf("reservation left %s", state)
		}
	}
	return
}

func TestAlternatingCharacterVoicesHashDeduplicateAndChargeNarrationOnly(t *testing.T) {
	h := setup(t)
	ctx := t.Context()
	queued, err := h.service.Enqueue(ctx, narrOwner, narrLesson)
	if err != nil || queued != 5 { // four segments plus the slide-level narration of sky-3
		t.Fatalf("queued %d %v", queued, err)
	}
	rows := h.settle(t, 4)
	for _, r := range rows {
		if r.Status != narration.StatusReady || r.Code != "" {
			t.Fatalf("not ready: %+v", r)
		}
		if r.Slide == "sky-1" && (r.Index == 0 || r.Index == 2) && (r.Speaker != "guide" || r.Actual != "voice-provisional-guide") {
			t.Fatalf("guide voice: %+v", r)
		}
		if r.Speaker == "supporter" && r.Actual != "voice-provisional-supporter" || r.Version != "2026-11-13.1" || r.Fallback != "" {
			t.Fatalf("supporter voice or version: %+v", r)
		}
	}
	// Order and identity survive: three segments of sky-1 in original order, identical guide lines share a hash.
	if rows[0].Hash != rows[2].Hash || rows[0].Hash == rows[1].Hash {
		t.Fatalf("hashes: %s %s %s", rows[0].Hash, rows[1].Hash, rows[2].Hash)
	}
	// The supporter's line is spoken twice (sky-1, sky-2) with one voice: one hash, one provider call.
	if rows[1].Hash != rows[3].Hash {
		t.Fatal("same text and voice must deduplicate across slides")
	}
	if got := h.twin.Calls(); got != 3 {
		t.Fatalf("provider calls %d, want 3 (one per distinct text and voice, plus the sky-3 slide narration)", got)
	}
	for _, d := range h.twin.SegmentTimeouts() {
		if d == 0 || d > 30000 {
			t.Fatalf("segment timeout %d", d)
		}
	}
	// Per-segment alignment covers each segment's own text.
	var alignmentKey string
	if err = h.f.DB.Pool.QueryRow(ctx, `SELECT alignment_key FROM slide_narrations WHERE lesson_id=$1 AND segment_slide_id='sky-1' AND segment_index=1`, narrLesson).Scan(&alignmentKey); err != nil {
		t.Fatal(err)
	}
	raw, err := h.objects.Get(ctx, alignmentKey)
	if err != nil || !strings.Contains(string(raw), `"characters"`) || strings.Count(string(raw), `","`) < len("Earth turns every day.")-1 {
		t.Fatalf("alignment %v %s", err, raw)
	}
	// One shared reservation for the whole Lesson, narration action only, priced from the Pricing config.
	settled, released, audio := h.spent(t)
	if settled != 1 || released != 0 || audio <= 0 {
		t.Fatalf("reservations settled=%d released=%d audio=%d", settled, released, audio)
	}
	var chars int
	if err = h.f.DB.Pool.QueryRow(ctx, `SELECT COALESCE(sum(characters),0) FROM narration_assets WHERE content_hash IN (SELECT content_hash FROM slide_narrations WHERE lesson_id=$1 AND status='ready' AND content_hash<>'' AND slide_id<>'seed-ready')`, narrLesson).Scan(&chars); err != nil || chars < len("Look closely at the sky.")+len("Earth turns every day.") {
		t.Fatal("characters", chars, err)
	}
	cfg, err := pricing.Load(ctx, h.f.DB.Queries)
	if err != nil {
		t.Fatal(err)
	}
	if want := narration.AggregateCredits([]int{chars}, cfg.Config().Narration.Credits, cfg.Config().Narration.Characters); audio != want {
		t.Fatalf("spent %d credits, want %d for %d new characters", audio, want, chars)
	}
	if len(h.events.named("character_voice_fallback")) != 0 {
		t.Fatal("approved voices must not report a fallback")
	}
	// Replaying is free: nothing is pending, so a second enqueue calls no provider and charges nothing.
	if again, e := h.service.Enqueue(ctx, narrOwner, narrLesson); e != nil || again != 0 || h.twin.Calls() != 3 {
		t.Fatalf("replay queued %d calls %d %v", again, h.twin.Calls(), e)
	}
}

func TestRetiredAndUnentitledVoicesUseTheDefaultForNewAudioOnly(t *testing.T) {
	h := setup(t)
	ctx := t.Context()
	pool := h.f.DB.Pool
	h.plan.entitled = false
	if _, err := h.service.Enqueue(ctx, narrOwner, narrLesson); err != nil {
		t.Fatal(err)
	}
	first := h.settle(t, 4)
	for _, r := range first {
		if r.Actual != "default" || r.Fallback != narration.ReasonNotEntitled || r.Status != narration.StatusReady {
			t.Fatalf("unentitled speaker must use the default voice: %+v", r)
		}
	}
	// One fallback event per speaker and reason, never names or text.
	events := h.events.named("character_voice_fallback")
	if len(events) != 2 {
		t.Fatalf("fallback events %v", events)
	}
	for _, e := range events {
		if len(e) != 7 && len(e) != 6 { // event + speaker_key,reason_code,voice_config_version + common source/platform/app_version
			t.Fatalf("unexpected properties %v", e)
		}
		for _, v := range e {
			if s, ok := v.(string); ok && (strings.Contains(s, "Look") || strings.Contains(s, "Earth")) {
				t.Fatalf("spoken text in event: %v", e)
			}
		}
	}
	// Entitled again with the guide voice retired: only new audio changes; stored audio stays unchanged.
	h.plan.entitled = true
	h.twin.RetireVoices("voice-provisional-guide")
	var oldHash string
	_ = pool.QueryRow(ctx, `SELECT content_hash FROM slide_narrations WHERE lesson_id=$1 AND segment_slide_id='sky-1' AND segment_index=0`, narrLesson).Scan(&oldHash)
	var storedBefore int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM narration_assets`).Scan(&storedBefore)
	if _, err := pool.Exec(ctx, `DELETE FROM slide_narrations WHERE lesson_id=$1 AND speaker_key<>'' AND segment_slide_id='sky-2'`, narrLesson); err != nil {
		t.Fatal(err)
	}
	if n, err := h.service.Enqueue(ctx, narrOwner, narrLesson); err != nil || n != 1 {
		t.Fatalf("new row %d %v", n, err)
	}
	rows := h.settle(t, 4)
	var again string
	_ = pool.QueryRow(ctx, `SELECT content_hash FROM slide_narrations WHERE lesson_id=$1 AND segment_slide_id='sky-1' AND segment_index=0`, narrLesson).Scan(&again)
	if again != oldHash {
		t.Fatal("old audio must stay unchanged by a voice change")
	}
	sky2 := rows[3]
	if sky2.Slide != "sky-2" || sky2.Actual != "voice-provisional-supporter" || sky2.Fallback != "" {
		t.Fatalf("supporter is still approved: %+v", sky2)
	}
	if audio, _, _ := func() (int, int, int) { s, r, a := h.spent(t); return s, r, a }(); audio < 1 {
		t.Fatal("expected narration reservations")
	}
	// Retire the supporter: the next new synthesis resolves the default and records the actual voice/version.
	supporterHash, callsBefore := sky2.Hash, h.twin.Calls()
	h.twin.RetireVoices("voice-provisional-guide", "voice-provisional-supporter")
	if _, err := pool.Exec(ctx, `DELETE FROM slide_narrations WHERE lesson_id=$1 AND speaker_key<>'' AND segment_slide_id='sky-2'`, narrLesson); err != nil {
		t.Fatal(err)
	}
	if n, err := h.service.Enqueue(ctx, narrOwner, narrLesson); err != nil || n != 1 {
		t.Fatalf("new row %d %v", n, err)
	}
	rows = h.settle(t, 4)
	sky2 = rows[3]
	if sky2.Actual != "default" || sky2.Fallback != narration.ReasonRetired || sky2.Version != "2026-11-13.1+retired" || sky2.Hash == supporterHash {
		t.Fatalf("retired supporter: %+v", sky2)
	}
	// The default-voice audio of the same text is already stored: reused for free, the old supporter audio untouched.
	if sky2.Hash != rows[1].Hash || h.twin.Calls() != callsBefore {
		t.Fatalf("fallback must reuse the stored default-voice audio without a provider call (%d -> %d)", callsBefore, h.twin.Calls())
	}
	var kept int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM narration_assets WHERE content_hash=$1 AND state='ready'`, supporterHash).Scan(&kept); err != nil || kept != 1 {
		t.Fatal("old audio must stay cached", kept, err)
	}
	if events := h.events.named("character_voice_fallback"); len(events) < 3 {
		t.Fatalf("retired fallback not reported: %v", events)
	}
}

func TestOutageKeepsCaptionsAndNarrationOffSkipsEverything(t *testing.T) {
	h := setup(t)
	ctx := t.Context()
	h.twin.SetFaults(map[string]string{"Earth turns every day.": "engine"})
	if _, err := h.service.Enqueue(ctx, narrOwner, narrLesson); err != nil {
		t.Fatal(err)
	}
	rows := h.settle(t, 4)
	for _, r := range rows {
		switch r.Speaker {
		case "supporter":
			if r.Status != narration.StatusFailed || r.Code != "engine_unavailable" || r.Hash != "" {
				t.Fatalf("supporter %+v", r)
			}
		default:
			if r.Status != narration.StatusReady {
				t.Fatalf("guide %+v", r)
			}
		}
	}
	// The captions never depend on audio: the segment text stays stored with the failed row.
	var captions int
	_ = h.f.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM slide_narrations WHERE lesson_id=$1 AND speaker_key<>'' AND narration_text<>''`, narrLesson).Scan(&captions)
	if captions != 4 {
		t.Fatalf("captions %d", captions)
	}
	settled, _, _ := h.spent(t)
	if settled != 1 {
		t.Fatalf("only the delivered guide audio is charged: %d", settled)
	}
	// Global narration-off: no rows, no calls, no charge.
	h2 := setup(t)
	if _, err := h2.f.DB.Pool.Exec(ctx, `UPDATE learner_profiles SET narration=false WHERE learner_id=$1`, narrOwner); err != nil {
		t.Fatal(err)
	}
	if n, err := h2.service.Enqueue(ctx, narrOwner, narrLesson); err != nil || n != 0 || h2.twin.Calls() != 0 || len(h2.rows(t)) != 0 {
		t.Fatalf("narration off queued %d calls %d %v", n, h2.twin.Calls(), err)
	}
}

func TestRemovedObjectsRestoreWithoutSynthesisAndAssetsStayReferenced(t *testing.T) {
	h := setup(t)
	ctx := t.Context()
	if _, err := h.service.Enqueue(ctx, narrOwner, narrLesson); err != nil {
		t.Fatal(err)
	}
	h.settle(t, 4)
	calls := h.twin.Calls()
	keys, err := narration.UnsharedObjectKeys(ctx, h.f.DB.Pool, []string{narrLesson})
	if err != nil || len(keys) < 4 {
		t.Fatalf("segment objects must be purged with the Lesson: %v %v", keys, err)
	}
	var refs int
	if err = h.f.DB.Pool.QueryRow(ctx, `SELECT COALESCE(sum(ref_count),0) FROM narration_assets a WHERE content_hash IN (SELECT content_hash FROM slide_narrations WHERE speaker_key<>'')`).Scan(&refs); err != nil || refs != 4 {
		t.Fatalf("every segment row references its asset: %d %v", refs, err)
	}
	if h.twin.Calls() != calls {
		t.Fatal("restoring or reading must never synthesize")
	}
}
