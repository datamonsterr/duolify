package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/narration"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

type passScreener struct{}

func (passScreener) ScreenCourse(context.Context, *pb.ScreenCourseRequest) (*pb.ScreenCourseResponse, error) {
	return &pb.ScreenCourseResponse{Verdict: "pass", ReasonCodes: []string{}, PolicyVersion: sharing.PolicyVersion,
		Attempts: []*pb.Usage{{Provider: "mock", Model: "mock", Attempt: 1, Delivered: true, InputTokens: 10, OutputTokens: 1}}}, nil
}

type e2eRates struct{}

func (e2eRates) Rates(context.Context) (sharing.Rates, error) {
	return sharing.Rates{Input: 1000, Output: 5000, CacheWrite: 1250, CacheRead: 100}, nil
}

type e2eAdult struct{}

func (e2eAdult) MeetsMinAge(context.Context, string, int) (bool, error) { return true, nil }

// TestSharedCopyPlaysOwnerNarrationAudioOverHTTP publishes a course whose owner has narration,
// enrolls a learner through the public share route and plays the owner's audio from the copy.
func TestSharedCopyPlaysOwnerNarrationAudioOverHTTP(t *testing.T) {
	auth, token, db := signedInDatabase(t)
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	learner, ctx, pool := claims.Subject, t.Context(), db.Pool
	objects := &publicationObjects{docs: map[string][]byte{}}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatalf("%v: %s", e, q)
		}
	}
	var owner, quote, creation, course, unit, stub string
	row := func(dest *string, q string, args ...any) {
		t.Helper()
		if e := pool.QueryRow(ctx, q, args...).Scan(dest); e != nil {
			t.Fatalf("%v: %s", e, q)
		}
	}
	row(&owner, `INSERT INTO learners(display_name) VALUES('Owner') RETURNING id::text`)
	exec(`INSERT INTO learner_profiles(learner_id,locale,time_zone,analytics_opt_out) VALUES($1,'en-US','UTC',false)`, owner)
	row(&quote, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) SELECT $1::uuid,encode(sha256(convert_to($1::text,'UTF8')),'hex'),version,'{}',now(),now()+interval '1 hour' FROM pricing_quote_policies ORDER BY version LIMIT 1 RETURNING id::text`, owner)
	row(&creation, `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1,gen_random_uuid(),1,$2,'{}','enrolled') RETURNING id::text`, owner, quote)
	row(&course, `INSERT INTO courses(learner_id,creation_id,title,description) VALUES($1,$2,'Astronomy','Look up.') RETURNING id::text`, owner, creation)
	exec(`INSERT INTO course_enrollments(learner_id,course_id) VALUES($1,$2)`, owner, course)
	row(&unit, `INSERT INTO course_units(learner_id,course_id,authored_id,position,title) VALUES($1,$2,'u1',0,'The Moon') RETURNING id::text`, owner, course)
	row(&stub, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1,$2,'l1',0,'Phases','Name phases','done') RETURNING id::text`, owner, unit)
	exec(`INSERT INTO generated_lessons(lesson_stub_id,course_id,learner_id,title,manifest) VALUES($1,$2,$3,'Phases','{}'::jsonb)`, stub, course, owner)
	exec(`INSERT INTO lesson_slides(lesson_stub_id,learner_id,position,slide_id,html,duration_ms,narration) VALUES($1,$2,0,'s0','<h1>Moon</h1><p>Fact.</p>',3000,'Narration text.')`, stub, owner)
	exec(`INSERT INTO lesson_questions(lesson_stub_id,learner_id,position,topic_id,prompt,choices,answer_index,explanation) VALUES($1,$2,0,'moon','Which one?','["Right","Wrong"]'::jsonb,0,'x')`, stub, owner)
	exec(`INSERT INTO lesson_manifests(lesson_id,learner_id,version,manifest_key) VALUES($1,$2,1,'lessons/owner/manifest.json')`, stub, owner)
	text := narration.NormalizeText("Narration text.")
	hash := narration.ContentHash(text, "voice-owner", "model-test", "fp-1")
	audio, alignment := narration.Keys(hash)
	exec(`INSERT INTO narration_assets(content_hash,text_hash,voice_id,model_id,settings_fingerprint,audio_key,alignment_key,characters,audio_bytes,duration_ms) VALUES($1,$2,'voice-owner','model-test','fp-1',$3,$4,15,9,3000)`, hash, narration.TextHash(text), audio, alignment)
	exec(`INSERT INTO slide_narrations(lesson_id,learner_id,slide_id,status,narration_text,content_hash,voice_id,model_id,settings_fingerprint,audio_key,alignment_key,characters,audio_bytes,duration_ms) VALUES($1,$2,'s0','ready',$3,$4,'voice-owner','model-test','fp-1',$5,$6,15,9,3000)`, stub, owner, text, hash, audio, alignment)
	objects.docs[audio] = []byte("RIFF-owner-audio")
	objects.docs[alignment] = []byte(`{"characters":[]}`)

	share := sharing.NewService(pool, nil, sharing.DefaultConfig()).WithAgeSource(e2eAdult{}).WithObjects(objects, playerfixture.AssetOrigin)
	share.ConfigureScreening(passScreener{}, e2eRates{})
	if d, e := share.BuildDraft(ctx, owner, course); e != nil || d.Verdict != sharing.VerdictPass {
		t.Fatalf("draft %+v %v", d, e)
	}
	link, err := share.PublishLink(ctx, owner, course)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Sharing: share, Player: player.New(player.Options{Pool: pool, Objects: objects, AssetOrigin: playerfixture.AssetOrigin})}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(server.Close)
	do := func(method, path, bearer string) (int, string) {
		t.Helper()
		req, e := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(""))
		if e != nil {
			t.Fatal(e)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}

	status, body := do("POST", "/v1/public/shares/"+link+"/enroll", token)
	if status != 201 {
		t.Fatalf("enroll %d %s", status, body)
	}
	var enrolled struct {
		CourseID string `json:"course_id"`
	}
	_ = json.Unmarshal([]byte(body), &enrolled)
	var lesson string
	row(&lesson, `SELECT l.id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND l.learner_id=$2 ORDER BY l.position LIMIT 1`, enrolled.CourseID, learner)

	url := fmt.Sprintf("/v1/lessons/%s/slides/s0/narration/", lesson)
	if status, body = do("GET", url+"audio", token); status != 200 || body != "RIFF-owner-audio" {
		t.Fatalf("copy audio %d %q", status, body)
	}
	if status, body = do("GET", url+"alignment", token); status != 200 || !strings.Contains(body, "characters") {
		t.Fatalf("copy alignment %d %q", status, body)
	}
	// Reuse is free: the learner has no reservation, quote or ledger entry.
	for _, q := range []string{`SELECT count(*) FROM credit_jobs WHERE learner_id=$1`, `SELECT count(*) FROM pricing_quotes WHERE learner_id=$1`} {
		var n int
		if e := pool.QueryRow(ctx, q, learner).Scan(&n); e != nil || n != 0 {
			t.Fatalf("%s = %d %v", q, n, e)
		}
	}
	// Anonymous callers and holders of other lessons get nothing; the owner's lesson is not the copy's.
	if status, _ = do("GET", url+"audio", ""); status == 200 {
		t.Fatal("anonymous audio")
	}
	if status, _ = do("GET", fmt.Sprintf("/v1/lessons/%s/slides/s0/narration/audio", stub), token); status != 404 {
		t.Fatalf("foreign lesson audio %d", status)
	}
	// A takedown that blocks the copy stops the audio.
	exec(`UPDATE course_copies SET blocked_at=now() WHERE copy_course_id=$1`, enrolled.CourseID)
	exec(`UPDATE slide_narrations SET status='disabled',failure_code='moderation_blocked' WHERE learner_id=$1 AND shared_draft_id IS NOT NULL`, learner)
	if status, _ = do("GET", url+"audio", token); status != 404 {
		t.Fatalf("blocked copy audio %d", status)
	}
}
