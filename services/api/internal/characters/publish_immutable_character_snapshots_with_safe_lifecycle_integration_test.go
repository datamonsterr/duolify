//go:build integration

package characters_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
	"github.com/datamonsterr/duolify/services/api/internal/steering"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestPublishedCharacterExportRequiresConfirmation(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	ctx := t.Context()
	owner, course := "a1111111-1111-4111-8111-111111111111", "f1111111-1111-4111-8111-111111111111"
	chars := characters.NewService(f.DB.Pool, f.ApplicationStorage(), f.Events())
	if _, err := chars.SelectCast(ctx, owner, course, characters.CastSelection{Guide: "default-guide"}); err != nil {
		t.Fatal(err)
	}
	s := sharing.NewService(f.DB.Pool, f.Events(), sharing.DefaultConfig())
	if _, err := s.BuildDraft(ctx, owner, course); err == nil || err.Error() != "character_export_confirmation_required" {
		t.Fatalf("export without informed confirmation: %v", err)
	}
}

func TestMissingPublishedLessonMaterializesOnce(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	ctx := t.Context()
	const owner = "a1111111-1111-4111-8111-111111111111"
	const lesson = "f3333333-3333-4333-8333-333333333333"
	if _, err := f.DB.Pool.Exec(ctx, `DELETE FROM lesson_manifests WHERE lesson_id=$1`, lesson); err != nil {
		t.Fatal(err)
	}
	p := player.New(player.Options{Pool: f.DB.Pool, Objects: f.ApplicationObjects(), AssetOrigin: playerfixture.AssetOrigin})
	if err := p.MaterializeGenerated(ctx, "a2222222-2222-4222-8222-222222222222", lesson); err == nil {
		t.Fatal("foreign materialization accepted")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for range 6 {
		wg.Go(func() { errs <- p.MaterializeGenerated(ctx, owner, lesson) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var key string
	if err := f.DB.Pool.QueryRow(ctx, `SELECT count(*),min(manifest_key) FROM lesson_manifests WHERE lesson_id=$1`, lesson).Scan(&count, &key); err != nil || count != 1 {
		t.Fatalf("manifest race: %d %v", count, err)
	}
	raw, err := f.ApplicationObjects().Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "answer") || strings.Contains(string(raw), "question") {
		t.Fatal("grading leaked into player objects")
	}
	versions, err := f.AssetOperator().ListVersions(ctx, key)
	if err != nil || len(versions) != 1 {
		t.Fatalf("objects not idempotent: %d %v", len(versions), err)
	}
	date := time.Now().UTC().Format(time.DateOnly)
	f.ProtectCharacterAssets(t, date)
	f.RunJob(t, date)
	restored := f.Restore(t, f.Ciphertext(t, f.ArchiveKey(date)))
	if err = f.ApplicationObjects().Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	f.RestoreCharacterAssets(t, date)
	recovered, err := f.ApplicationObjects().Get(ctx, key)
	if err != nil || string(recovered) != string(raw) {
		t.Fatal("generated manifest not restored from protected bytes", err)
	}
	var restoredKey string
	if err = restored.Pool.QueryRow(ctx, `SELECT manifest_key FROM lesson_manifests WHERE lesson_id=$1`, lesson).Scan(&restoredKey); err != nil || restoredKey != key {
		t.Fatal("generated pin not restored", err)
	}
}

type publicationEngine struct {
	pb.UnimplementedEngineServiceServer
	down  atomic.Bool
	calls atomic.Int32
}

func (e *publicationEngine) ScreenCourse(context.Context, *pb.ScreenCourseRequest) (*pb.ScreenCourseResponse, error) {
	return &pb.ScreenCourseResponse{Verdict: "pass", PolicyVersion: sharing.PolicyVersion, Attempts: []*pb.Usage{{Provider: "mock", Model: "screen", Attempt: 1, Delivered: true, InputTokens: 10, OutputTokens: 3}}}, nil
}
func (e *publicationEngine) ScreenCharacter(ctx context.Context, r *pb.ScreenCharacterRequest) (*pb.ScreenCharacterResponse, error) {
	e.calls.Add(1)
	if e.down.Load() {
		return nil, errors.New("screen unavailable")
	}
	if !r.PublicationExport || r.RawPrompt != "" || r.CompiledAppearance == "" || r.Limits == nil || r.Limits.TimeoutMs > 10000 || r.Limits.DeadlineUnixMs == 0 || r.Limits.MaxAttempts > 2 {
		return nil, errors.New("invalid publication envelope")
	}
	out, err := (screeningDouble{}).ScreenCharacter(ctx, r)
	if out != nil {
		out.PublicationScreened = true
	}
	return out, err
}

type publicationRPC struct{ pb.EngineServiceClient }

func (c publicationRPC) ScreenCourse(ctx context.Context, r *pb.ScreenCourseRequest) (*pb.ScreenCourseResponse, error) {
	return c.EngineServiceClient.ScreenCourse(ctx, r)
}
func (c publicationRPC) ScreenCharacter(ctx context.Context, r *pb.ScreenCharacterRequest) (*pb.ScreenCharacterResponse, error) {
	return c.EngineServiceClient.ScreenCharacter(ctx, r)
}
func publicationClient(t *testing.T, e *publicationEngine) publicationRPC {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	pb.RegisterEngineServiceServer(server, e)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///publication", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return publicationRPC{pb.NewEngineServiceClient(conn)}
}

type publicationRates struct{}

type publicationSteering struct{}

func (publicationSteering) ScreenInstruction(context.Context, *pb.ScreenInstructionRequest) (*pb.ScreenInstructionResponse, error) {
	return &pb.ScreenInstructionResponse{Allowed: true, ScreeningVersion: "steering-v1", Attempts: []*pb.Usage{{Provider: "mock", Model: "screen", Attempt: 1, Delivered: true, InputTokens: 10, OutputTokens: 2}}}, nil
}

func (publicationRates) Rates(context.Context) (sharing.Rates, error) {
	return sharing.Rates{Input: 1000, Output: 5000, CacheWrite: 1250, CacheRead: 100}, nil
}

type publicationAdult struct{}

func (publicationAdult) MeetsMinAge(context.Context, string, int) (bool, error) { return true, nil }

type publicationEvents struct {
	mu     sync.Mutex
	events []string
	props  []map[string]any
	owners []string
}

func (e *publicationEvents) Capture(owner, event string, props map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
	e.props = append(e.props, props)
	e.owners = append(e.owners, owner)
	return nil
}
func (*publicationEvents) Close() error { return nil }

func TestPublishedCharacterIndependentReuseScreeningAndRestore(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	ctx := t.Context()
	owner, viewer, course, id := "a1111111-1111-4111-8111-111111111111", "a7777777-7777-4777-8777-777777777777", "f1111111-1111-4111-8111-111111111111", "17111111-1111-4111-8111-111111111111"
	if _, err := f.DB.Pool.Exec(ctx, `WITH inserted AS (INSERT INTO learners(id,display_name) VALUES($1,'Snapshot learner') RETURNING id) INSERT INTO learner_profiles(learner_id,analytics_opt_out) SELECT id,false FROM inserted`, viewer); err != nil {
		t.Fatal(err)
	}
	chars := characters.NewService(f.DB.Pool, f.ApplicationStorage(), nil)
	if _, err := f.DB.Pool.Exec(ctx, `UPDATE learner_profiles SET analytics_opt_out=false WHERE learner_id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	chars.ConfigureScreening(newScreeningRPC(t))
	if _, err := chars.SelectCast(ctx, owner, course, characters.CastSelection{Guide: id, Supporting: []string{"default-supporter"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := chars.PinCast(ctx, owner, course, "publication-generated"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.DB.Pool.Exec(ctx, `UPDATE generated_lessons SET manifest=manifest||'{"cast_key":"publication-generated"}'::jsonb WHERE course_id=$1`, course); err != nil {
		t.Fatal(err)
	}
	engine := new(publicationEngine)
	events := new(publicationEvents)
	cfg := sharing.DefaultConfig()
	cfg.CharacterExportsEnabled = true
	service := sharing.NewService(f.DB.Pool, events, cfg).WithAgeSource(publicationAdult{}).WithObjects(f.ApplicationObjects(), playerfixture.AssetOrigin).WithAssetStore(f.ApplicationObjects()).WithBlockLedger(backup.ModerationLedger{Objects: f.Store("writer")})
	service.ConfigureScreening(publicationClient(t, engine), publicationRates{})
	confirmed := sharing.WithCharacterExportConfirmation(ctx, true)
	draft, err := service.BuildDraft(confirmed, owner, course)
	if err != nil || draft.Verdict != "pass" {
		t.Fatalf("draft %+v %v", draft, err)
	}
	var original []byte
	if err = f.DB.Pool.QueryRow(ctx, `SELECT snapshot FROM publication_drafts WHERE id=$1`, draft.ID).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(original), id) || strings.Contains(string(original), owner) || sharing.CheckAllowlist(original) != nil {
		t.Fatal("private identity or out-of-allowlist content leaked")
	}
	_, err = service.PublishLink(ctx, owner, course)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.PublishLink(ctx, owner, course); err != nil {
		t.Fatal(err)
	} // Same version emits once.
	// The rotation above revokes the first token; obtain a current link for enrollment.
	token, err := service.RotateLink(ctx, owner, course)
	if err != nil {
		t.Fatal(err)
	}
	enrolled, err := service.Enroll(ctx, viewer, token)
	if err != nil {
		t.Fatal(err)
	}
	var beforeCredits int
	if err = f.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_entries WHERE learner_id=$1`, viewer).Scan(&beforeCredits); err != nil {
		t.Fatal(err)
	}
	pinned, err := chars.PinCast(ctx, viewer, enrolled.CourseID, "beyond-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if pinned.CastSource != "published" || !strings.HasPrefix(pinned.Speakers["guide"].CharacterID, "snapshot-") {
		t.Fatalf("copy used private library: %+v", pinned)
	}
	before, _ := json.Marshal(pinned)
	if _, err = chars.SelectCast(ctx, viewer, enrolled.CourseID, characters.CastSelection{Guide: "default-guide"}); !errors.Is(err, characters.ErrPublishedCast) {
		t.Fatalf("unforked override accepted: %v", err)
	}
	current, err := chars.Get(ctx, owner, id)
	if err != nil {
		t.Fatal(err)
	}
	current.Spec.Info.Name = "New version guide"
	raw, _ := json.Marshal(current.Spec)
	if _, err = chars.Edit(ctx, owner, id, characters.EditRequest{ExpectedVersion: 1, Spec: raw}); err != nil {
		t.Fatal(err)
	}
	next, err := service.Republish(confirmed, owner, course, "New guide")
	if err != nil || next.Verdict != "pass" {
		t.Fatalf("republish %+v %v", next, err)
	}
	same, err := chars.PinCast(ctx, viewer, enrolled.CourseID, "new-job-before-update")
	if err != nil {
		t.Fatal(err)
	}
	sameRaw, _ := json.Marshal(same)
	if string(sameRaw) != string(before) {
		t.Fatal("library edit or republish changed copy pin")
	}
	engine.down.Store(true)
	held, err := service.Republish(confirmed, owner, course, "Unavailable screen")
	if err != nil || held.Verdict != "pending_screening" {
		t.Fatalf("not fail closed: %+v %v", held, err)
	}
	engine.down.Store(false)
	// E15 pinning keeps completed old Lessons while the future Course Cast takes the new version.
	if _, err = f.DB.Pool.Exec(ctx, `UPDATE course_lesson_stubs st SET status='done' FROM course_units u WHERE st.unit_id=u.id AND u.course_id=$1`, enrolled.CourseID); err != nil {
		t.Fatal(err)
	}
	update, err := service.UpdateStatus(ctx, viewer, enrolled.CourseID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApplyUpdate(ctx, viewer, enrolled.CourseID, update.LockVersion, next.Number); err != nil {
		t.Fatal(err)
	}
	future, err := chars.PinCast(ctx, viewer, enrolled.CourseID, "after-update")
	if err != nil || future.Speakers["guide"].Spec.Info.Name != "New version guide" {
		t.Fatalf("future Cast not updated %+v %v", future, err)
	}
	steer := steering.New(steering.Options{Pool: f.DB.Pool, Engine: publicationSteering{}})
	instruction := steering.Input{Text: "More concrete examples", Scope: "rest_of_course"}
	if _, err = steer.Add(ctx, viewer, enrolled.CourseID, instruction); steering.Code(err) != "fork_confirmation_required" {
		t.Fatalf("fork skipped confirmation: %v", err)
	}
	instruction.ConfirmFork = true
	if _, err = steer.Add(ctx, viewer, enrolled.CourseID, instruction); err != nil {
		t.Fatal(err)
	}
	if _, err = chars.SelectCast(ctx, viewer, enrolled.CourseID, characters.CastSelection{Guide: "default-companion"}); err != nil {
		t.Fatalf("confirmed fork cannot select future Cast: %v", err)
	}
	ownedFuture, err := chars.PinCast(ctx, viewer, enrolled.CourseID, "fork-owned-future")
	if err != nil || ownedFuture.Speakers["guide"].CharacterID != "default-companion" {
		t.Fatalf("fork failed: %+v %v", ownedFuture, err)
	}
	if err = chars.Delete(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	old, err := chars.PinCast(ctx, viewer, enrolled.CourseID, "beyond-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	oldRaw, _ := json.Marshal(old)
	if string(oldRaw) != string(before) {
		t.Fatal("library deletion mutated copy")
	}
	if err = service.Unpublish(ctx, owner, course); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Enroll(ctx, "a3333333-3333-4333-8333-333333333333", token); err == nil {
		t.Fatal("unpublish allowed enrollment")
	}
	date := time.Now().UTC().Format(time.DateOnly)
	f.ProtectCharacterAssets(t, date)
	f.RunJob(t, date)
	ciphertext := f.Ciphertext(t, f.ArchiveKey(date))
	ledger := backup.DeletionLedger{Objects: f.Store("writer")}
	if err = ledger.Append(ctx, accounts.LearnerReference(owner)); err != nil {
		t.Fatal(err)
	}
	if err = accounts.ReplayDeletionLedger(ctx, f.DB.Pool, ledger); err != nil {
		t.Fatal(err)
	}
	calls := engine.calls.Load()
	restored := f.Restore(t, ciphertext)
	if err = accounts.ReplayDeletionLedger(ctx, restored.Pool, ledger); err != nil {
		t.Fatal(err)
	}
	recovered := characters.NewService(restored.Pool, f.ApplicationStorage(), nil)
	originalPin, err := recovered.PinCast(ctx, viewer, enrolled.CourseID, "beyond-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	recoveredRaw, _ := json.Marshal(originalPin)
	if string(recoveredRaw) != string(before) {
		t.Fatal("restore required deleted library or changed pin")
	}
	// Takedown ledger is deliberately newer than the dump, and must win on restore.
	snapshotID := pinned.Speakers["guide"].CharacterID
	moderator := "a2222222-2222-4222-8222-222222222222"
	if _, err = f.DB.Pool.Exec(ctx, `INSERT INTO moderator_grants(account_id) VALUES($1) ON CONFLICT DO NOTHING`, moderator); err != nil {
		t.Fatal(err)
	}
	if err = service.SuppressCharacter(ctx, moderator, snapshotID, "unsafe"); err != nil {
		t.Fatal(err)
	}
	if err = backup.ReplayCharacterSuppressions(ctx, restored.Pool, f.Store("restorer")); err != nil {
		t.Fatal(err)
	}
	safe, err := recovered.PinCast(ctx, viewer, enrolled.CourseID, "beyond-snapshot")
	if err != nil || !safe.FallbackUsed || safe.Speakers["guide"].CharacterID != "default-guide" {
		t.Fatalf("takedown not replayed: %+v %v", safe, err)
	}
	var persisted []byte
	if err = restored.Pool.QueryRow(ctx, `SELECT snapshot FROM lesson_cast_snapshots WHERE course_id=$1 AND lesson_key='beyond-snapshot'`, enrolled.CourseID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	var stored characters.LessonCastSnapshot
	_ = json.Unmarshal(persisted, &stored)
	storedRaw, _ := json.Marshal(stored)
	if string(storedRaw) != string(before) {
		t.Fatal("suppression rewrote stored payload")
	}
	keys, err := f.ApplicationObjects().List(ctx, "lessons/character-rigs/")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if err = f.ApplicationObjects().Delete(ctx, key); err != nil {
			t.Fatal(err)
		}
	}
	f.RestoreCharacterAssets(t, date)
	for _, key := range keys {
		ok, e := f.ApplicationObjects().Exists(ctx, key)
		if e != nil || !ok {
			t.Fatalf("protected asset not restored %s %v", key, e)
		}
	}
	if engine.calls.Load() != calls {
		t.Fatal("restore invoked paid screening")
	}
	// A fresh recount under the hash lock must retain assets during concurrent job pinning.
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	wg.Go(func() { _, e := service.SweepAssets(ctx, time.Now()); failures <- e })
	wg.Go(func() {
		tx, e := f.DB.Pool.Begin(ctx)
		if e != nil {
			failures <- e
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if e = characters.PinAssetReferences(ctx, tx, pinned); e == nil {
			_, e = tx.Exec(ctx, `INSERT INTO lesson_cast_snapshots(course_id,learner_id,lesson_key,snapshot) VALUES($1,$2,'concurrent-retention-pin',$3)`, enrolled.CourseID, viewer, before)
		}
		if e == nil {
			e = tx.Commit(ctx)
		}
		failures <- e
	})
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, key := range keys {
		ok, e := f.ApplicationObjects().Exists(ctx, key)
		if e != nil || !ok {
			t.Fatal("shared referenced asset purged", key, e)
		}
	}
	if _, err = f.DB.Pool.Exec(ctx, `UPDATE courses SET deleted_at=now() WHERE id=$1`, enrolled.CourseID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = service.SweepAssets(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err = service.SweepAssets(ctx, now.Add(29*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		ok, e := f.ApplicationObjects().Exists(ctx, key)
		if e != nil || !ok {
			t.Fatal("asset lost before 30-day grace", key, e)
		}
	}
	if _, err = service.SweepAssets(ctx, now.Add(31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		ok, e := f.ApplicationObjects().Exists(ctx, key)
		if e != nil || ok {
			t.Fatal("unreferenced asset retained past purge", key, e)
		}
	}
	var afterCredits int
	if err = f.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_entries WHERE learner_id=$1`, viewer).Scan(&afterCredits); err != nil {
		t.Fatal(err)
	}
	if beforeCredits != afterCredits {
		t.Fatal("reuse/export added Character charge")
	}
	events.mu.Lock()
	defer events.mu.Unlock()
	published := 0
	for i, event := range events.events {
		if event == "character_snapshot_published" {
			published++
			raw, _ := json.Marshal(events.props[i])
			if strings.Contains(string(raw), "New version guide") || events.owners[i] != owner || events.props[i]["source"] != "api" || events.props[i]["platform"] != "web" {
				t.Fatalf("unsafe analytics %s", raw)
			}
		}
	}
	if published != 2 {
		t.Fatalf("publication events=%d want 2", published)
	}
}
