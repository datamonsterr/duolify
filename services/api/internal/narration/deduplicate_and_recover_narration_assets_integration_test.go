//go:build integration

package narration_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/narration"
	"github.com/datamonsterr/duolify/services/api/internal/narration/narrationtest"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type assetHarness struct {
	t       *testing.T
	ctx     context.Context
	pool    *pgxpool.Pool
	objects interface {
		Get(context.Context, string) ([]byte, error)
		Put(context.Context, string, []byte) error
	}
	mock    *narrationtest.Engine
	events  *recorder
	service *narration.Service
	opts    narration.Options
	texts   []string
	chars   []int
}

func newAssetHarness(t *testing.T, retention time.Duration) *assetHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	t.Cleanup(cancel)
	f := backuptest.New(t)
	f.Seed(t)
	// The backup fixture stores narration=false; these tests exercise a Learner who has narration on.
	if _, err := f.DB.Pool.Exec(t.Context(), `UPDATE learner_profiles SET narration=true,narration_voice='default' WHERE learner_id=$1`, "a1111111-1111-4111-8111-111111111111"); err != nil {
		t.Fatal(err)
	}
	h := &assetHarness{t: t, ctx: ctx, pool: f.DB.Pool, objects: f.ApplicationObjects(), mock: &narrationtest.Engine{}, events: &recorder{}}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(server, h.mock)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	catalogue, err := pricing.Load(ctx, f.DB.Queries)
	if err != nil {
		t.Fatal(err)
	}
	financial := credits.NewServiceWithCatalogue(h.pool, nil, catalogue, h.events)
	if _, err = h.pool.Exec(ctx, `INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance) VALUES ($1, 500, 'purchase', 'asset-topup:' || gen_random_uuid(), '2026.04', '{"bucket": "purchase"}'::jsonb)`, owner); err != nil {
		t.Fatal(err)
	}
	// The shared fixture leaves one open reservation; release it for the concurrency bound.
	rows, err := h.pool.Query(ctx, `SELECT id::text FROM credit_jobs WHERE learner_id=$1 AND state='reserved'`, owner)
	if err != nil {
		t.Fatal(err)
	}
	var open []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		open = append(open, id)
	}
	rows.Close()
	for _, id := range open {
		if _, err = financial.Release(ctx, owner, id); err != nil {
			t.Fatal(err)
		}
	}
	h.opts = narration.Options{Pool: h.pool, Objects: h.objects, Speech: enginev1.NewEngineServiceClient(conn), Financial: financial, Events: h.events, JobAttempts: 1, Retention: retention}
	h.service = narration.New(h.opts)
	// Jobs are driven synchronously through SynthesizeLesson; the queue only accepts inserts.
	if _, err = h.service.Queue(); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"The sky is a map waiting to be explored.", "Stars are distant suns, shining across space.", "Notice how the sky changes as Earth turns."} {
		h.texts = append(h.texts, text)
		h.chars = append(h.chars, len([]rune(text)))
	}
	// Narration of the seed row stays; drop it so counts only reflect this test.
	if _, err = h.pool.Exec(ctx, `DELETE FROM slide_narrations WHERE slide_id='seed-ready'`); err != nil {
		t.Fatal(err)
	}
	if _, err = h.pool.Exec(ctx, `DELETE FROM narration_assets WHERE content_hash IN (repeat('ab',32),repeat('ef',32))`); err != nil {
		t.Fatal(err)
	}
	return h
}

// plainObjects hides bucket-version restore so only retained settings versions can recover.
type plainObjects struct {
	o interface {
		Get(context.Context, string) ([]byte, error)
		Put(context.Context, string, []byte) error
	}
}

func (p plainObjects) Get(ctx context.Context, k string) ([]byte, error) { return p.o.Get(ctx, k) }
func (p plainObjects) Put(ctx context.Context, k string, v []byte) error { return p.o.Put(ctx, k, v) }

func (h *assetHarness) enqueue(lessonID string) {
	h.t.Helper()
	// Slides are stored pending by Enqueue; River stays idle because Start is never called.
	if n, err := h.service.Enqueue(h.ctx, owner, lessonID); err != nil || n != 3 {
		h.t.Fatal(n, err)
	}
}

func (h *assetHarness) spent() int {
	h.t.Helper()
	var n int
	if err := h.pool.QueryRow(h.ctx, `SELECT COALESCE(sum((j.result->>'audio_spent')::int),0) FROM credit_jobs j JOIN pricing_quotes q ON q.id=j.quote_id WHERE j.learner_id=$1 AND q.snapshot->>'action'='narration'`, owner).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

func (h *assetHarness) jobs() int {
	h.t.Helper()
	var n int
	_ = h.pool.QueryRow(h.ctx, `SELECT count(*) FROM credit_jobs j JOIN pricing_quotes q ON q.id=j.quote_id WHERE j.learner_id=$1 AND q.snapshot->>'action'='narration'`, owner).Scan(&n)
	return n
}

func (h *assetHarness) status(lessonID, slide string) string {
	var st string
	_ = h.pool.QueryRow(h.ctx, `SELECT status FROM slide_narrations WHERE lesson_id=$1 AND slide_id=$2`, lessonID, slide).Scan(&st)
	return st
}

func ceilCredits(characters int) int { return (characters*50 + 1439) / 1440 }

func TestDeduplicateAndRecoverNarrationAssetsIntegration(t *testing.T) {
	h := newAssetHarness(t, 30*24*time.Hour)
	ctx, pool := h.ctx, h.pool
	twin, err := playerfixture.Twin(ctx, pool, owner, lesson)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Aggregate pricing: one reservation for the lesson, newly synthesized characters
	// rounded up once; the failed slide releases nothing it did not deliver.
	h.mock.SetFaults(map[string]string{h.texts[2]: "engine"})
	h.enqueue(lesson)
	if err = h.service.SynthesizeLesson(ctx, owner, lesson, true); err != nil {
		t.Fatal(err)
	}
	if h.status(lesson, "sky-1") != "ready" || h.status(lesson, "sky-2") != "ready" || h.status(lesson, "sky-3") != "failed" {
		t.Fatal("statuses", h.status(lesson, "sky-1"), h.status(lesson, "sky-2"), h.status(lesson, "sky-3"))
	}
	delivered := h.chars[0] + h.chars[1]
	if h.jobs() != 1 || h.spent() != ceilCredits(delivered) {
		// Per-slide rounding would give ceil(a)+ceil(b); aggregate rounding is smaller or equal.
		t.Fatalf("aggregate charge: jobs=%d spent=%d want=%d", h.jobs(), h.spent(), ceilCredits(delivered))
	}
	if ceilCredits(h.chars[0])+ceilCredits(h.chars[1]) == ceilCredits(delivered) {
		t.Log("fixture texts do not distinguish aggregate from per-slide rounding")
	}

	// 2. Retry of the failed slide reuses the same hash and charges only the new delivery.
	h.mock.SetFaults(nil)
	state, err := h.service.Retry(ctx, owner, lesson, "sky-3")
	if err != nil || state != "pending" {
		t.Fatal(state, err)
	}
	calls := h.mock.Calls()
	if err = h.service.Synthesize(ctx, owner, lesson, "sky-3", true); err != nil || h.status(lesson, "sky-3") != "ready" {
		t.Fatal("retry", err)
	}
	if h.mock.Calls() != calls+1 {
		t.Fatal("retry must call the provider once", h.mock.Calls()-calls)
	}
	if h.spent() != ceilCredits(delivered)+ceilCredits(h.chars[2]) {
		t.Fatal("retry charged", h.spent())
	}

	// 3. Concurrent identical lessons: no provider call, no charge, shared references.
	before, spentBefore := h.mock.Calls(), h.spent()
	h.enqueue(twin)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := h.service.SynthesizeLesson(ctx, owner, twin, true); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if h.mock.Calls() != before || h.spent() != spentBefore || len(h.events.named("narration_cache_hit")) != 3 {
		t.Fatal("identical narration must be free", h.mock.Calls()-before, h.spent()-spentBefore)
	}
	rows, _ := pool.Query(ctx, `SELECT ref_count FROM narration_assets ORDER BY content_hash`)
	var refCounts []int
	for rows.Next() {
		var n int
		_ = rows.Scan(&n)
		refCounts = append(refCounts, n)
	}
	rows.Close()
	if fmt.Sprint(refCounts) != "[2 2 2]" {
		t.Fatal("reference counts", refCounts)
	}
	for _, e := range h.events.events {
		for _, text := range h.texts {
			if strings.Contains(fmt.Sprint(e), text) {
				t.Fatal("learner text reached analytics", e["event"])
			}
		}
	}

	// 4. Object restored from the bucket's retained version: no provider call, no charge.
	var audioKey string
	_ = pool.QueryRow(ctx, `SELECT audio_key FROM slide_narrations WHERE lesson_id=$1 AND slide_id='sky-1'`, lesson).Scan(&audioKey)
	if err = h.objects.(narration.Deleter).Delete(ctx, audioKey); err != nil {
		t.Fatal(err)
	}
	if _, err = h.objects.Get(ctx, audioKey); err == nil {
		t.Fatal("object must be gone")
	}
	calls, spentBefore = h.mock.Calls(), h.spent()
	if state, err = h.service.Recover(ctx, owner, lesson, "sky-1"); err != nil || state != "restored" {
		t.Fatal(state, err)
	}
	if raw, e := h.objects.Get(ctx, audioKey); e != nil || !bytes.HasPrefix(raw, []byte("RIFF")) {
		t.Fatal("version restore", e)
	}

	// 5. Missing object restored from a retained prior settings version of the same text.
	var textHash, hash string
	_ = pool.QueryRow(ctx, `SELECT text_hash, content_hash FROM narration_assets a WHERE EXISTS(SELECT 1 FROM slide_narrations n WHERE n.content_hash=a.content_hash AND n.slide_id='sky-2' AND n.lesson_id=$1)`, lesson).Scan(&textHash, &hash)
	prior := strings.Repeat("cd", 32)
	priorAudio, priorAlignment := narration.Keys(prior)
	var rawAudio, rawAlignment []byte
	var alignKey string
	_ = pool.QueryRow(ctx, `SELECT audio_key, alignment_key FROM narration_assets WHERE content_hash=$1`, hash).Scan(&audioKey, &alignKey)
	rawAudio, _ = h.objects.Get(ctx, audioKey)
	rawAlignment, _ = h.objects.Get(ctx, alignKey)
	_ = h.objects.Put(ctx, priorAudio, rawAudio)
	_ = h.objects.Put(ctx, priorAlignment, rawAlignment)
	if _, err = pool.Exec(ctx, `INSERT INTO narration_assets(content_hash,text_hash,voice_id,model_id,settings_fingerprint,audio_key,alignment_key,characters,audio_bytes,duration_ms)
		SELECT $2,text_hash,voice_id,model_id,'fp0-old-settings',$3,$4,characters,audio_bytes,duration_ms FROM narration_assets WHERE content_hash=$1`, hash, prior, priorAudio, priorAlignment); err != nil {
		t.Fatal(err)
	}
	if err = h.objects.(narration.Deleter).Delete(ctx, audioKey); err != nil {
		t.Fatal(err)
	}
	if err = h.objects.(narration.Deleter).Delete(ctx, alignKey); err != nil {
		t.Fatal(err)
	}
	// Hide bucket versions so only the prior settings version can serve the Learner.
	opts := h.opts
	opts.Objects = plainObjects{h.objects}
	plain := narration.New(opts)
	if state, err = plain.Recover(ctx, owner, lesson, "sky-2"); err != nil || state != "restored" {
		t.Fatal(state, err)
	}
	var now string
	_ = pool.QueryRow(ctx, `SELECT content_hash FROM slide_narrations WHERE lesson_id=$1 AND slide_id='sky-2'`, lesson).Scan(&now)
	if now != prior {
		t.Fatal("slide must point at the retained prior version", now)
	}
	if h.mock.Calls() != calls || h.spent() != spentBefore {
		t.Fatal("restore must not call the provider or charge", h.mock.Calls()-calls)
	}

	// 6. Nothing to restore from: tracked operator regeneration, never charged.
	_ = pool.QueryRow(ctx, `SELECT audio_key, alignment_key FROM narration_assets WHERE content_hash=(SELECT content_hash FROM slide_narrations WHERE lesson_id=$1 AND slide_id='sky-3')`, lesson).Scan(&audioKey, &alignKey)
	_ = h.objects.(narration.Deleter).Delete(ctx, audioKey)
	if state, err = plain.Recover(ctx, owner, lesson, "sky-3"); err != nil || state != "regenerating" {
		t.Fatal(state, err)
	}
	if h.status(lesson, "sky-3") != "pending" || h.status(twin, "sky-3") != "ready" {
		// The twin still references the same asset; it recovers with it.
		t.Log("twin status", h.status(twin, "sky-3"))
	}
	var open int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM narration_regenerations WHERE status='requested'`).Scan(&open)
	if open != 1 {
		t.Fatal("tracked regeneration", open)
	}
	if again, e := plain.Recover(ctx, owner, lesson, "sky-3"); e != nil || again != "regenerating" {
		t.Fatal(again, e)
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM narration_regenerations`).Scan(&open)
	if open != 1 {
		t.Fatal("regeneration must be tracked once per hash", open)
	}
	if n, e := plain.RegenerateMissing(ctx); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if h.status(lesson, "sky-3") != "ready" || h.status(twin, "sky-3") != "ready" || h.mock.Calls() != calls+1 || h.spent() != spentBefore {
		t.Fatal("operator regeneration", h.mock.Calls()-calls, h.spent()-spentBefore)
	}
	if _, e := h.objects.Get(ctx, audioKey); e != nil {
		t.Fatal("regenerated object", e)
	}
	var done string
	_ = pool.QueryRow(ctx, `SELECT status FROM narration_regenerations`).Scan(&done)
	if done != "completed" {
		t.Fatal(done)
	}

	// 7. References: deleting one lesson keeps shared objects; the last reference starts
	// retention and the object is deleted only after it elapses.
	if _, err = pool.Exec(ctx, `DELETE FROM slide_narrations WHERE lesson_id=$1`, twin); err != nil {
		t.Fatal(err)
	}
	var refs int
	_ = pool.QueryRow(ctx, `SELECT ref_count FROM narration_assets WHERE content_hash=(SELECT content_hash FROM slide_narrations WHERE lesson_id=$1 AND slide_id='sky-3')`, lesson).Scan(&refs)
	if refs != 1 {
		t.Fatal("reference count after deleting a sharer", refs)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM slide_narrations WHERE lesson_id=$1`, lesson); err != nil {
		t.Fatal(err)
	}
	if n, e := h.service.PurgeUnreferenced(ctx); e != nil || n != 0 {
		t.Fatal("retention must hold unreferenced assets", n, e)
	}
	if _, err = pool.Exec(ctx, `UPDATE narration_assets SET unreferenced_at=now()-interval '31 days' WHERE ref_count=0`); err != nil {
		t.Fatal(err)
	}
	if n, e := h.service.PurgeUnreferenced(ctx); e != nil || n < 3 {
		t.Fatal("purge after retention", n, e)
	}
	if _, e := h.objects.Get(ctx, audioKey); e == nil {
		t.Fatal("purged object must be gone")
	}
}
