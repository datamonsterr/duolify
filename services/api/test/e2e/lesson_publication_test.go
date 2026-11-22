package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
)

type publicationObjects struct {
	mu       sync.Mutex
	docs     map[string][]byte
	readOnly bool
}

func (o *publicationObjects) Get(_ context.Context, key string) ([]byte, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	raw, ok := o.docs[key]
	if !ok {
		return nil, errors.New("missing object")
	}
	return raw, nil
}

func (o *publicationObjects) Put(_ context.Context, key string, raw []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.readOnly {
		return errors.New("playback must not write objects")
	}
	o.docs[key] = raw
	return nil
}

type publicationEngine struct{}

func (publicationEngine) GenerateLesson(_ context.Context, req *enginev1.GenerateLessonRequest) (*enginev1.Lesson, []*enginev1.Usage, error) {
	return &enginev1.Lesson{
		Slides: []*enginev1.Slide{{SlideId: "generated-sky", Html: `<div id="root">Generated sky</div>`, DurationMs: 1000, Narration: "Stars are distant suns.", TopicIds: []string{"stars"},
			Regions: []*enginev1.SlideRegion{
				{RegionId: "title", Box: &enginev1.RegionBox{X: 0.05, Y: 0.04, W: 0.9, H: 0.18}, Role: enginev1.RegionRole_REGION_ROLE_TITLE, Description: "Title: Generated sky"},
				{RegionId: "sky", Box: &enginev1.RegionBox{X: 0.05, Y: 0.26, W: 0.9, H: 0.52}, Window: &enginev1.RegionWindow{StartMs: 200, EndMs: 1000}, Role: enginev1.RegionRole_REGION_ROLE_DIAGRAM, Description: "Diagram of distant suns"},
			}}},
		Test:        []*enginev1.Question{{QuestionId: "q1", TopicId: "stars", Prompt: "What are stars?", Choices: []string{"Suns", "Clouds"}, Explanation: "Stars are suns."}},
		VariantPool: []*enginev1.TopicVariants{{TopicId: "stars", Variants: []*enginev1.Question{{QuestionId: "v1", TopicId: "stars", Prompt: "What shines at night?", Choices: []string{"Distant suns", "Clouds"}, Explanation: "Distant suns shine."}}}},
	}, []*enginev1.Usage{{Provider: req.Limits.Provider, Model: req.Limits.Model, InputTokens: 100, OutputTokens: 20, Attempt: 1, Component: "content", Delivered: true}}, nil
}

func TestGeneratedLessonPublishedBeforePlayback(t *testing.T) {
	auth, token, db := signedInDatabase(t)
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	owner := claims.Subject
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err = worker.Migrate(t.Context(), db.Pool, logger); err != nil {
		t.Fatal(err)
	}
	objects := &publicationObjects{docs: map[string][]byte{}}
	lesson, err := playerfixture.Seed(t.Context(), db.Pool, objects, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(t.Context(), `DELETE FROM lesson_manifests WHERE lesson_id=$1`, lesson); err != nil {
		t.Fatal(err)
	}
	cat, err := pricing.Load(t.Context(), db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	financial := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)
	if _, err = db.Pool.Exec(t.Context(), `INSERT INTO credit_entries(learner_id,amount,kind,reference) VALUES($1,1000,'purchase','publication-e2e')`, owner); err != nil {
		t.Fatal(err)
	}
	service := lessontest.New(lessontest.Options{Objects: objects, AssetOrigin: playerfixture.AssetOrigin, Pool: db.Pool, Engine: publicationEngine{}, Financial: financial})
	queue, err := service.LessonQueue(logger)
	if err != nil {
		t.Fatal(err)
	}
	if err = queue.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = queue.Stop(ctx)
	})
	server := httptest.NewUnstartedServer(httpapi.NewWithLessonTest(httpapi.Dependencies{Accounts: auth, Quotes: pricing.NewQuotes(db.Pool), Credits: financial, LessonTestTracerEnabled: true, Player: player.New(player.Options{Pool: db.Pool, Objects: objects, AssetOrigin: playerfixture.AssetOrigin})}, service, logger))
	_ = server.Listener.Close()
	server.Listener, err = new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:4040")
	if err != nil {
		t.Fatal(err)
	}
	server.Start()
	t.Cleanup(server.Close)
	call := func(method, path, body string, want int) map[string]any {
		t.Helper()
		req, e := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		if resp.StatusCode != want {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, raw)
		}
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		if out == nil {
			out = map[string]any{}
		}
		out["raw_response"] = string(raw)
		return out
	}
	q := call("POST", "/v1/quotes", `{"action":"lesson_content","narration_characters":100}`, 200)["id"].(string)
	call("POST", "/v1/quotes/"+q+"/accept", `{}`, 200)
	id := call("POST", "/v1/lessons/"+lesson+"/prepare", `{"quote_id":"`+q+`"}`, 202)["id"].(string)
	deadline := time.Now().Add(15 * time.Second)
	for {
		var state string
		if err = db.Pool.QueryRow(t.Context(), `SELECT state FROM lesson_generations WHERE id=$1`, id).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("generation did not complete: %s", state)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var key string
	if err = db.Pool.QueryRow(t.Context(), `SELECT manifest_key FROM lesson_manifests WHERE lesson_id=$1`, lesson).Scan(&key); err != nil {
		t.Fatalf("settled lesson has no published manifest before playback: %v", err)
	}
	stored, err := objects.Get(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	// The stored Slide manifest carries the validated regions for server-side grounding.
	var object player.ObjectManifest
	if err = json.Unmarshal(stored, &object); err != nil {
		t.Fatal(err)
	}
	regions := object.Slides[0].Regions
	if len(regions) != 2 || regions[0].Role != "title" || regions[1].Window == nil || regions[1].Window.StartMS != 200 || regions[1].Box.H != 0.52 {
		t.Fatalf("published regions %+v", regions)
	}
	if got := player.RegionsAt(object.Slides[0], player.RegionBox{X: 0.1, Y: 0.3, W: 0.3, H: 0.3}, 500); len(got) != 1 || got[0].Region.ID != "sky" {
		t.Fatalf("intersection %+v", got)
	}
	objects.mu.Lock()
	objects.readOnly = true
	objects.mu.Unlock()
	manifest := call("GET", "/v1/lessons/"+lesson+"/manifest", "", 200)
	// Nothing new reaches the client: the playback manifest has no regions.
	if strings.Contains(manifest["raw_response"].(string), "region") {
		t.Fatal("playback manifest exposes regions", manifest["raw_response"])
	}
	slide := manifest["slides"].([]any)[0].(map[string]any)
	if slide["slide_id"] != "generated-sky" {
		t.Fatal("player delivered a different deck", slide)
	}
	html := call("GET", slide["html_url"].(string), "", 200)["raw_response"].(string)
	if !strings.Contains(html, "Generated sky") {
		t.Fatal("player did not deliver the generated slide", html)
	}
}
