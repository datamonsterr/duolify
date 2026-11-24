package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type coursePlannerEngine struct {
	enginev1.UnimplementedEngineServiceServer
	requests chan *enginev1.GenerateLessonRequest
}

func e2eLessonPlan(id string) *enginev1.LessonPlan {
	plan := &enginev1.LessonPlan{
		Objective:      "Explain the Moon's phases",
		KeyPoints:      []string{"The Sun lights half the Moon", "Our view changes as the Moon orbits"},
		Misconceptions: []string{"Earth's shadow causes ordinary phases"},
		ExampleHint:    "Model the orbit with a lamp and ball",
	}
	if id == "eclipses" {
		plan.Objective = "Distinguish solar and lunar eclipses"
		plan.PrerequisiteLessonIds = []string{"phases"}
		plan.ReviewLessonIds = []string{"phases"}
	}
	return plan
}

func (*coursePlannerEngine) GenerateCourseOutline(_ context.Context, req *enginev1.GenerateCourseOutlineRequest) (*enginev1.GenerateCourseOutlineResponse, error) {
	outline := proto.CloneOf(req.GetEditedOutline())
	for _, unit := range outline.GetUnits() {
		for _, lesson := range unit.GetLessons() {
			lesson.Plan = e2eLessonPlan(lesson.GetLessonId())
		}
	}
	return &enginev1.GenerateCourseOutlineResponse{
		Outline: outline, OutlineRevision: req.GetOutlineRevision(),
		Attempts: []*enginev1.Usage{{Provider: req.Limits.Provider, Model: req.Limits.Model, InputTokens: 100, OutputTokens: 20, Attempt: 1, Component: "content", Delivered: true}},
	}, nil
}

func (e *coursePlannerEngine) GenerateLesson(req *enginev1.GenerateLessonRequest, stream grpc.ServerStreamingServer[enginev1.GenerateLessonEvent]) error {
	e.requests <- req
	lesson, attempts, err := (publicationEngine{}).GenerateLesson(stream.Context(), req)
	if err != nil {
		return err
	}
	for _, attempt := range attempts {
		if err := stream.Send(&enginev1.GenerateLessonEvent{Event: &enginev1.GenerateLessonEvent_Attempt{Attempt: attempt}}); err != nil {
			return err
		}
	}
	return stream.Send(&enginev1.GenerateLessonEvent{Event: &enginev1.GenerateLessonEvent_Lesson{Lesson: lesson}})
}

func TestConfirmedCourseStoresPlansAndPassesFirstPlanToEngine(t *testing.T) {
	auth, access, db := signedInDatabase(t)
	claims, err := auth.ParseAccess(access)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := worker.Migrate(ctx, db.Pool, logger); err != nil {
		t.Fatal(err)
	}
	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,1000,'purchase','course-plan-e2e',$2)`, claims.Subject, catalogue.Config().Version); err != nil {
		t.Fatal(err)
	}
	backend := grpc.NewServer()
	mock := &coursePlannerEngine{requests: make(chan *enginev1.GenerateLessonRequest, 1)}
	enginev1.RegisterEngineServiceServer(backend, mock)
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = backend.Serve(listener) }()
	t.Cleanup(backend.Stop)
	client, err := engine.New("http://"+listener.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	financial := credits.NewServiceWithCatalogue(db.Pool, nil, catalogue, nil)
	creation := courses.New(courses.Options{Pool: db.Pool, OutlineEngine: client, Financial: financial})
	lessons := lessontest.New(lessontest.Options{
		Pool: db.Pool, Engine: client, Financial: financial, AssetOrigin: playerfixture.AssetOrigin,
		Objects: &publicationObjects{docs: map[string][]byte{}},
	})
	server := httptest.NewServer(httpapi.NewWithLessonTest(httpapi.Dependencies{
		Accounts: auth, CourseBrief: creation, Quotes: pricing.NewQuotes(db.Pool), Credits: financial, LessonTestTracerEnabled: true,
	}, lessons, logger))
	t.Cleanup(server.Close)
	type result struct {
		ID       string                     `json:"id"`
		CourseID string                     `json:"course_id"`
		State    string                     `json:"state"`
		Tables   map[string]json.RawMessage `json:"tables"`
	}
	call := func(method, path string, body any, want int) result {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+access)
		req.Header.Set("Content-Type", "application/json")
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, err = io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != want {
			t.Fatalf("%s %s: %d %s (%v)", method, path, resp.StatusCode, raw, err)
		}
		var out result
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	post := http.MethodPost
	brief := call(post, "/v1/course-brief/confirm", json.RawMessage(`{"confirmed":true,"brief":{"topic":"Moon"}}`), 201)
	draft := call(post, "/v1/outline-drafts", map[string]any{
		"brief_revision": brief.ID,
		"outline": json.RawMessage(`[{"id":"moon","title":"Moon","lessons":[
			{"id":"phases","title":"Phases","goal":"Explain phases"},
			{"id":"eclipses","title":"Eclipses","goal":"Compare eclipses"}
		]}]`),
	}, 201)
	call(post, "/v1/outline-drafts/"+draft.ID+"/confirm", map[string]int{"expected_revision": 1}, 200)
	in := map[string]any{"draft_id": draft.ID, "expected_revision": 1}
	quote := call(post, "/v1/course-creations/quote", in, 201).ID
	call(post, "/v1/quotes/"+quote+"/accept", struct{}{}, 200)
	in["quote_id"] = quote
	creationID := call(post, "/v1/course-creations", in, 202).ID
	if err := creation.RunCreation(ctx, creationID); err != nil {
		t.Fatal(err)
	}
	course := call(http.MethodGet, "/v1/course-creations/"+creationID, nil, 200)
	if course.State != "enrolled" {
		t.Fatalf("course creation did not enroll: %v", course)
	}
	var firstID string
	for _, tc := range []struct{ id, status string }{{"phases", "ready_to_generate"}, {"eclipses", "locked"}} {
		var id, status string
		var raw []byte
		if err := db.Pool.QueryRow(ctx, `SELECT l.id::text,l.status,l.plan FROM course_lesson_stubs l
			JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND l.authored_id=$2`, course.CourseID, tc.id).Scan(&id, &status, &raw); err != nil {
			t.Fatal(err)
		}
		var plan enginev1.LessonPlan
		if err := protojson.Unmarshal(raw, &plan); err != nil || !proto.Equal(&plan, e2eLessonPlan(tc.id)) || status != tc.status {
			t.Fatalf("stub %s: status %s, plan %s, error %v", tc.id, status, raw, err)
		}
		if tc.id == "phases" {
			firstID = id
		}
	}
	var generated int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM generated_lessons WHERE course_id=$1`, course.CourseID).Scan(&generated); err != nil || generated != 0 {
		t.Fatalf("outline created lesson content: %d (%v)", generated, err)
	}
	exported := call(http.MethodGet, "/v1/me/data-export", nil, 200)
	var stubs []struct {
		ID   string          `json:"id"`
		Plan json.RawMessage `json:"plan"`
	}
	if err := json.Unmarshal(exported.Tables["course_lesson_stubs"], &stubs); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, stub := range stubs {
		if stub.ID == firstID {
			var plan enginev1.LessonPlan
			if err := protojson.Unmarshal(stub.Plan, &plan); err != nil || !proto.Equal(&plan, e2eLessonPlan("phases")) {
				t.Fatalf("exported first plan: %s (%v)", stub.Plan, err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("first lesson stub missing from account export")
	}
	quote = call(post, "/v1/quotes", json.RawMessage(`{"action":"lesson_content","narration_characters":100}`), 200).ID
	call(post, "/v1/quotes/"+quote+"/accept", struct{}{}, 200)
	generation := call(post, "/v1/lessons/"+firstID+"/prepare", map[string]string{"quote_id": quote}, 202).ID
	if err := lessons.RunLessonGeneration(ctx, generation); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-mock.requests:
		if !proto.Equal(req.GetLesson().GetPlan(), e2eLessonPlan("phases")) {
			t.Fatalf("lesson request plan: %v", req.GetLesson().GetPlan())
		}
	default:
		t.Fatal("lesson generation did not reach the engine")
	}
}
