//go:build integration

package sharing_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/narration"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

// ownerNarration gives every generated slide of the owner's course one ready narration (E14) and
// returns the content hash by slide id. Equal text shares one asset, as the E14 dedupe does.
func ownerNarration(t *testing.T, f *backuptest.Fixture, ownerID, course string) map[string]string {
	t.Helper()
	ctx, pool := t.Context(), f.DB.Pool
	rows, err := pool.Query(ctx, `SELECT s.lesson_stub_id::text,s.slide_id,s.narration FROM lesson_slides s JOIN course_lesson_stubs st ON st.id=s.lesson_stub_id JOIN course_units u ON u.id=st.unit_id WHERE u.course_id=$1 ORDER BY s.lesson_stub_id,s.position`, course)
	if err != nil {
		t.Fatal(err)
	}
	type slide struct{ stub, id, text string }
	var slides []slide
	for rows.Next() {
		var s slide
		if err = rows.Scan(&s.stub, &s.id, &s.text); err != nil {
			t.Fatal(err)
		}
		slides = append(slides, s)
	}
	rows.Close()
	out := map[string]string{}
	for _, s := range slides {
		text := narration.NormalizeText(s.text)
		hash := narration.ContentHash(text, "voice-owner", "model-test", "fp-1")
		audio, alignment := narration.Keys(hash)
		if _, err = pool.Exec(ctx, `INSERT INTO lesson_manifests(lesson_id,learner_id,version,manifest_key) VALUES($1,$2,1,$3) ON CONFLICT DO NOTHING`, s.stub, ownerID, "lessons/owner/"+s.stub+"/manifest.json"); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO narration_assets(content_hash,text_hash,voice_id,model_id,settings_fingerprint,audio_key,alignment_key,characters,audio_bytes,duration_ms) VALUES($1,$2,'voice-owner','model-test','fp-1',$3,$4,$5,100,3000) ON CONFLICT DO NOTHING`, hash, narration.TextHash(text), audio, alignment, len([]rune(text))); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO slide_narrations(lesson_id,learner_id,slide_id,status,narration_text,content_hash,voice_id,model_id,settings_fingerprint,audio_key,alignment_key,characters,audio_bytes,duration_ms) VALUES($1,$2,$3,'ready',$4,$5,'voice-owner','model-test','fp-1',$6,$7,$8,100,3000)`, s.stub, ownerID, s.id, text, hash, audio, alignment, len([]rune(text))); err != nil {
			t.Fatal(err)
		}
		out[s.id] = hash
	}
	return out
}

// distinctNarration gives slide s1 its own text so its narration asset differs from s0's.
func distinctNarration(t *testing.T, f *backuptest.Fixture) {
	t.Helper()
	if _, err := f.DB.Pool.Exec(t.Context(), `UPDATE lesson_slides SET narration='Second narration.' WHERE slide_id='s1'`); err != nil {
		t.Fatal(err)
	}
}

// delObjects records deletions so retention tests see which narration objects went.
type delObjects struct {
	mu      sync.Mutex
	deleted []string
}

func (d *delObjects) Get(context.Context, string) ([]byte, error) { return nil, nil }
func (d *delObjects) Put(context.Context, string, []byte) error   { return nil }
func (d *delObjects) Delete(_ context.Context, key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleted = append(d.deleted, key)
	return nil
}

func TestPublishedSnapshotCarriesOwnerNarrationAudioRefs(t *testing.T) {
	f := newSeededSharingFixture(t)
	ctx := t.Context()
	s, _ := enrollService(t, f, &capture{}, nil)
	src := seedReusable(t, f, "Astronomy")
	distinctNarration(t, f)
	hashes := ownerNarration(t, f, owner, src)
	// A narration whose text changed after it was made is stale and must not be published.
	if _, err := f.DB.Pool.Exec(ctx, `UPDATE lesson_slides SET narration='Edited after synthesis.' WHERE slide_id='s1'`); err != nil {
		t.Fatal(err)
	}
	// An asset whose object is gone is not referenced; the learner regenerates it as before.
	if _, err := f.DB.Pool.Exec(ctx, `UPDATE narration_assets SET state='missing' WHERE content_hash=$1`, hashes["s0"]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.DB.Pool.Exec(ctx, `UPDATE narration_assets SET state='ready' WHERE content_hash=$1`, hashes["s0"]); err != nil {
		t.Fatal(err)
	}
	d, err := s.BuildDraft(ctx, owner, src)
	if err != nil || d.Verdict != sharing.VerdictPass || !d.NarrationIncl {
		t.Fatalf("draft=%+v err=%v", d, err)
	}
	var raw string
	if err = f.DB.Pool.QueryRow(ctx, `SELECT snapshot::text FROM publication_drafts WHERE id=$1`, d.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, hashes["s0"]) {
		t.Fatalf("ready narration of s0 not referenced: %s", raw)
	}
	if strings.Contains(raw, hashes["s1"]) {
		t.Error("stale narration referenced")
	}
	for _, banned := range []string{"audio_key", "alignment_key", "lessons/narration", "voice-owner", "model-test", owner} {
		if strings.Contains(raw, banned) {
			t.Errorf("snapshot leaks %q", banned)
		}
	}
	if n := count(t, f, `SELECT count(*) FROM published_narration_pins WHERE draft_id=$1`, d.ID); n != 1 {
		t.Fatalf("pins=%d, want the single referenced hash", n)
	}

	// A missing asset at publish time is left out.
	if _, err = f.DB.Pool.Exec(ctx, `UPDATE narration_assets SET state='missing' WHERE content_hash=$1`, hashes["s0"]); err != nil {
		t.Fatal(err)
	}
	d2, err := s.BuildDraft(ctx, owner, src)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.DB.Pool.QueryRow(ctx, `SELECT snapshot::text FROM publication_drafts WHERE id=$1`, d2.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, hashes["s0"]) {
		t.Error("missing asset referenced")
	}
}

func TestSharedCopyReusesOwnerAudioWithoutNarrationSpend(t *testing.T) {
	f := newSeededSharingFixture(t)
	ctx := t.Context()
	s, _ := enrollService(t, f, &capture{}, nil)
	consent(t, f, stranger, false)
	second := newLearner(t, f, "Second learner")
	src := seedReusable(t, f, "Astronomy")
	distinctNarration(t, f)
	hashes := ownerNarration(t, f, owner, src)
	token := publish(t, s, src)
	assets := count(t, f, `SELECT count(*) FROM narration_assets`)

	got, err := s.Enroll(ctx, stranger, token)
	if err != nil {
		t.Fatal(err)
	}
	rows := `SELECT count(*) FROM slide_narrations n JOIN course_lesson_stubs l ON l.id=n.lesson_id JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND n.learner_id=$2 AND n.status='ready'`
	if n := count(t, f, rows, got.CourseID, stranger); n != 2 {
		t.Fatalf("copied narrations=%d, want one per ready lesson", n)
	}
	var hash, audio, text string
	var duration int
	if err = f.DB.Pool.QueryRow(ctx, `SELECT n.content_hash,n.audio_key,n.narration_text,n.duration_ms FROM slide_narrations n JOIN course_lesson_stubs l ON l.id=n.lesson_id JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 AND n.slide_id='s0'`, got.CourseID).Scan(&hash, &audio, &text, &duration); err != nil {
		t.Fatal(err)
	}
	if hash != hashes["s0"] || !strings.HasPrefix(audio, "lessons/narration/") || text != "Narration text." || duration != 3000 {
		t.Fatalf("copied row %s %s %q %d", hash, audio, text, duration)
	}
	// Same asset, one more reference per copy: deduplicated by content hash.
	if n := count(t, f, `SELECT count(*) FROM narration_assets`); n != assets {
		t.Fatalf("assets %d -> %d", assets, n)
	}
	if n := count(t, f, `SELECT ref_count FROM narration_assets WHERE content_hash=$1`, hashes["s0"]); n != 2 {
		t.Fatalf("ref_count=%d, want owner + copy", n)
	}
	// No generation, quote, reservation or charge for reused audio.
	for _, q := range []string{`SELECT count(*) FROM pricing_quotes WHERE learner_id=$1`, `SELECT count(*) FROM credit_jobs WHERE learner_id=$1`, `SELECT count(*) FROM credit_entries WHERE learner_id=$1`} {
		if n := count(t, f, q, stranger); n != 0 {
			t.Fatalf("%s = %d", q, n)
		}
	}
	if n := count(t, f, `SELECT count(*) FROM narration_regenerations`); n != 0 {
		t.Fatalf("regenerations queued: %d", n)
	}
	if n := count(t, f, `SELECT count(*) FROM slide_narrations WHERE learner_id=$1 AND status<>'ready'`, stranger); n != 0 {
		t.Fatalf("pending narration rows for the learner: %d", n)
	}

	// Another learner shares the same asset.
	if _, err = s.Enroll(ctx, second, token); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f, `SELECT ref_count FROM narration_assets WHERE content_hash=$1`, hashes["s0"]); n != 3 {
		t.Fatalf("ref_count=%d after second copy", n)
	}

	// Deleting a copy releases only its own reference.
	if _, err = f.DB.Pool.Exec(ctx, `DELETE FROM slide_narrations WHERE learner_id=$1`, second); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f, `SELECT ref_count FROM narration_assets WHERE content_hash=$1`, hashes["s0"]); n != 2 {
		t.Fatalf("ref_count=%d after one copy dropped", n)
	}
}

func TestMissingOwnerAudioFallsBackToLearnerRegeneration(t *testing.T) {
	f := newSeededSharingFixture(t)
	ctx := t.Context()
	s, _ := enrollService(t, f, &capture{}, nil)
	consent(t, f, stranger, false)
	src := seedReusable(t, f, "Astronomy")
	distinctNarration(t, f)
	hashes := ownerNarration(t, f, owner, src)
	token := publish(t, s, src)
	// The object disappears between publish and enroll: no reuse, existing per-learner path.
	if _, err := f.DB.Pool.Exec(ctx, `UPDATE narration_assets SET state='missing' WHERE content_hash=$1`, hashes["s1"]); err != nil {
		t.Fatal(err)
	}
	got, err := s.Enroll(ctx, stranger, token)
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, f, `SELECT count(*) FROM slide_narrations WHERE learner_id=$1 AND slide_id='s1'`, stranger); n != 0 {
		t.Fatalf("missing asset copied: %d rows", n)
	}
	if n := count(t, f, `SELECT count(*) FROM slide_narrations WHERE learner_id=$1 AND slide_id='s0' AND status='ready'`, stranger); n != 1 {
		t.Fatal("available audio must still be reused")
	}
	if got.ReadyLessons != 2 {
		t.Fatalf("lessons must stay playable without audio: %+v", got)
	}
}

func TestSevereTakedownDisablesCopyAudioAndLiftRestoresIt(t *testing.T) {
	f := newSharingFixture(t)
	r := startRedis(t)
	s := marketService(t, f, &capture{}, r, nil)
	mod := grantModerator(t, f, "Enforcer")
	ownerID := marketOwner(t, f, "audio-owner")
	course := marketCourse(t, f, ownerID, "Unsafe audio", "Short lessons", 3)
	hashes := ownerNarration(t, f, ownerID, course)
	listCourse(t, s, ownerID, course, "tech", "en")
	if _, err := s.PublishLink(t.Context(), ownerID, course); err != nil {
		t.Fatal(err)
	}
	learner := newLearner(t, f, "Copy holder")
	_, err := s.EnrollMarketplace(t.Context(), learner, course)
	if err != nil {
		t.Fatal(err)
	}
	var hash string
	for _, h := range hashes {
		hash = h
	}
	ready := func() int {
		return count(t, f, `SELECT count(*) FROM slide_narrations WHERE learner_id=$1 AND status='ready'`, learner)
	}
	if ready() == 0 {
		t.Fatal("copy holds no reused audio")
	}
	refs := count(t, f, `SELECT ref_count FROM narration_assets WHERE content_hash=$1`, hash)
	item := reportedCourse(t, f, s, ownerID, course, "unsafe")
	res := act(t, s, mod, item.ID, "takedown", "unsafe")
	if !res.Severe || res.CopiesBlocked != 1 {
		t.Fatalf("takedown=%+v", res)
	}
	if ready() != 0 {
		t.Fatal("blocked copy still has servable audio")
	}
	if n := count(t, f, `SELECT ref_count FROM narration_assets WHERE content_hash=$1`, hash); n >= refs {
		t.Fatalf("ref_count %d -> %d: blocked copy must release its audio reference", refs, n)
	}
	// Overturned on appeal: the same audio is served again.
	appeal, err := s.FileAppeal(t.Context(), ownerID, res.ID, "The content is fine.")
	if err != nil {
		t.Fatal(err)
	}
	second := grantModerator(t, f, "Second enforcer")
	if err = s.DecideAppeal(t.Context(), second, appeal, "overturn", "No violation found."); err != nil {
		t.Fatal(err)
	}
	if ready() == 0 {
		t.Fatal("lifted block did not restore audio")
	}
}

func TestPinnedAudioOutlivesOwnerRowsAndIsPurgedAfterRetention(t *testing.T) {
	f := newSeededSharingFixture(t)
	ctx, pool := t.Context(), f.DB.Pool
	s, _ := enrollService(t, f, &capture{}, nil)
	src := seedReusable(t, f, "Astronomy")
	distinctNarration(t, f)
	hashes := ownerNarration(t, f, owner, src)
	publish(t, s, src)
	objects := &delObjects{}
	svc := narration.New(narration.Options{Pool: pool, Objects: objects, Retention: time.Hour})
	hash := hashes["s0"]

	// The owner's own rows go away (lesson deleted / account erasure); the published snapshot needs the audio.
	if _, err := pool.Exec(ctx, `DELETE FROM slide_narrations WHERE learner_id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE narration_assets SET unreferenced_at=now()-interval '40 days' WHERE ref_count=0`); err != nil {
		t.Fatal(err)
	}
	// Other unreferenced fixture assets may go; the pinned one must not.
	if _, err := svc.PurgeUnreferenced(ctx); err != nil || strings.Contains(strings.Join(objects.deleted, ","), hash) {
		t.Fatalf("published audio purged: err=%v deleted=%v", err, objects.deleted)
	}
	if n := count(t, f, `SELECT count(*) FROM narration_assets WHERE content_hash=$1`, hash); n != 1 {
		t.Fatal("asset row gone")
	}

	// Unpublished: the sweep starts the retention window, so nothing is deleted yet.
	if err := s.Unpublish(ctx, owner, src); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := s.SweepAssets(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PurgeUnreferenced(ctx); err != nil || strings.Contains(strings.Join(objects.deleted, ","), hash) {
		t.Fatalf("purged inside the window: err=%v", err)
	}
	// After the window the audio goes by the E14 policy.
	if _, err := pool.Exec(ctx, `UPDATE published_narration_pins SET unpinned_at=now()-interval '40 days' WHERE unpinned_at IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PurgeUnreferenced(ctx); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(objects.deleted, ","); !strings.Contains(got, hash) {
		t.Fatalf("objects deleted: %s", got)
	}
	if n := count(t, f, `SELECT count(*) FROM published_narration_pins WHERE content_hash=$1`, hash); n != 0 {
		t.Fatal("pin outlived its asset")
	}
}
