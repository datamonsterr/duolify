package e2e

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/narration"
	"github.com/datamonsterr/duolify/services/api/internal/narration/narrationtest"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"google.golang.org/grpc"
)

type cf02Speech struct{ *narrationtest.Engine }

func (s cf02Speech) SynthesizeNarration(ctx context.Context, req *pb.SynthesizeNarrationRequest, _ ...grpc.CallOption) (*pb.SynthesizeNarrationResponse, error) {
	return s.Engine.SynthesizeNarration(ctx, req)
}

func TestOpenRouterNarrationPlaybackFallbackAndFreeReuse(t *testing.T) {
	auth, token, db := signedInDatabase(t)
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := worker.Migrate(ctx, db.Pool, logger); err != nil {
		t.Fatal(err)
	}
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	owner := claims.Subject
	objects := &playerfixture.MemoryObjects{}
	lesson, err := playerfixture.Seed(ctx, db.Pool, objects, owner)
	if err != nil {
		t.Fatal(err)
	}
	twin, err := playerfixture.Twin(ctx, db.Pool, owner, lesson)
	if err != nil {
		t.Fatal(err)
	}
	var doc player.ObjectManifest
	if err = json.Unmarshal(playerfixture.Documents()[playerfixture.ManifestKey], &doc); err != nil {
		t.Fatal(err)
	}
	doc.Slides[0].NarrationText = "Cafe\u0301   shadows change."
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	const key = "lessons/fixture-cf-02/manifest.json"
	if err = objects.Put(ctx, key, raw); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE lesson_manifests SET manifest_key=$3 WHERE lesson_id IN ($1,$2)`, lesson, twin, key); err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `UPDATE learner_profiles SET narration=true WHERE learner_id=$1`, owner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version,provenance)
		VALUES($1,100,'purchase','cf-02','2026.04','{"bucket":"purchase"}')`, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = narrationtest.ActivateOpenRouterCostPolicy(ctx, db.Pool); err != nil {
		t.Fatal(err)
	}
	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	engine := &narrationtest.Engine{OpenRouter: true}
	engine.SetFaults(map[string]string{
		narration.NormalizeText(doc.Slides[1].NarrationText): "openrouter_outage",
		narration.NormalizeText(doc.Slides[2].NarrationText): "engine",
	})
	options := narration.Options{Pool: db.Pool, Objects: objects, Speech: cf02Speech{engine}, Logger: logger,
		OpenRouterBudget: narration.OpenRouterBudget{MonthlyCapNanos: 5_000_000_000},
		Budget:           narration.Budget{Limit: 40000, StopPercent: 95, CycleDay: 1}, Financial: credits.NewServiceWithCatalogue(db.Pool, nil, catalogue, nil)}
	service := narration.New(options)
	if _, err = service.Queue(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{lesson, twin} {
		if n, err := service.Enqueue(ctx, owner, id); err != nil || n != 3 {
			t.Fatalf("enqueue %s: %d, %v", id, n, err)
		}
		if err = service.SynthesizeLesson(ctx, owner, id, true); err != nil {
			t.Fatal(err)
		}
	}
	handler := httpapi.New(httpapi.Dependencies{Accounts: auth, Player: player.New(player.Options{
		Pool: db.Pool, Objects: objects, AssetOrigin: playerfixture.AssetOrigin,
	})}, logger)
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(ctx, "GET", "/v1/lessons/"+lesson+path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := get("/manifest")
	var m player.Manifest
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &m) != nil || len(m.Slides) != 3 {
		t.Fatalf("manifest %d %s", w.Code, w.Body)
	}
	for i, want := range []string{"ready", "ready", "failed"} {
		if m.Slides[i].NarrationStatus != want {
			t.Fatalf("slide %d status %s, expected %s", i, m.Slides[i].NarrationStatus, want)
		}
	}
	for i, source := range []string{"estimated", "vendor"} {
		w = get("/slides/" + m.Slides[i].ID + "/narration/alignment")
		var a narration.Alignment
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &a) != nil || a.TimingSource != source || len(a.EndMS) == 0 ||
			int(a.EndMS[len(a.EndMS)-1]) != a.DurationMS || strings.Join(a.Characters, "") != m.Slides[i].NarrationText {
			t.Fatalf("alignment %d: %d %s", i, w.Code, w.Body)
		}
	}
	var providers string
	var charges int
	if err = db.Pool.QueryRow(ctx, `SELECT string_agg(a.provider,',' ORDER BY n.lesson_id,n.slide_id)
		FROM slide_narrations n JOIN narration_assets a USING(content_hash) WHERE n.learner_id=$1 AND n.status='ready'`, owner).Scan(&providers); err != nil {
		t.Fatal(err)
	}
	if providers != "openrouter,elevenlabs,openrouter,elevenlabs" {
		t.Fatalf("actual providers %s", providers)
	}
	if err = db.Pool.QueryRow(ctx, `SELECT COALESCE(sum((result->>'audio_spent')::int),0) FROM credit_jobs WHERE learner_id=$1`, owner).Scan(&charges); err != nil {
		t.Fatal(err)
	}
	want := narration.AggregateCredits([]int{len([]rune(m.Slides[0].NarrationText)), len([]rune(m.Slides[1].NarrationText))}, 50, 1440)
	if charges != want {
		t.Fatalf("charged %d credits, expected %d for only newly delivered audio", charges, want)
	}
	var maiAttempts int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*)
		FROM credit_usage u JOIN credit_jobs j ON j.id=u.job_id CROSS JOIN LATERAL jsonb_array_elements(u.usage) a
		WHERE j.learner_id=$1 AND a->>'model'='microsoft/mai-voice-2.1-flash' AND a->>'fallback_used'='true'`, owner).Scan(&maiAttempts); err != nil || maiAttempts == 0 {
		t.Fatalf("missing MAI fallback attempts: %d %v", maiAttempts, err)
	}

	var before, after int
	const quoteCount = `SELECT count(*) FROM pricing_quotes WHERE learner_id=$1 AND snapshot->>'action'='narration'`
	if err = db.Pool.QueryRow(ctx, quoteCount, owner).Scan(&before); err != nil {
		t.Fatal(err)
	}
	slide := m.Slides[2].ID
	if _, err = service.Retry(ctx, owner, lesson, slide); err != nil {
		t.Fatal(err)
	}
	calls := engine.Calls()
	options.OpenRouterBudget = narration.OpenRouterBudget{}
	options.Budget = narration.Budget{Limit: 1, StopPercent: 100, CycleDay: 1}
	if err = narration.New(options).Synthesize(ctx, owner, lesson, slide, true); err != nil {
		t.Fatal(err)
	}
	w = get("/manifest")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &m) != nil ||
		m.Slides[2].NarrationStatus != "disabled" || m.Slides[2].NarrationReason != narration.CodeBudgetExhausted ||
		m.Slides[2].NarrationText == "" || engine.Calls() != calls {
		t.Fatalf("exhausted budgets didn't leave captions without a provider call: %d %s", w.Code, w.Body)
	}
	if err = db.Pool.QueryRow(ctx, quoteCount, owner).Scan(&after); err != nil || after != before {
		t.Fatalf("budget rejection created a learner quote: before %d after %d err %v", before, after, err)
	}
}
