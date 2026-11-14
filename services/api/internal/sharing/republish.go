package sharing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrNotPublished means the Course has no live passing version to republish over.
	ErrNotPublished = errors.New("not_published")
	// ErrRepublishLimit means the Course used its republishes for the rolling day.
	ErrRepublishLimit = errors.New("republish_limit")
	// ErrUpdateUnavailable is the one neutral refusal for a copy update: unknown, foreign, forked,
	// already current or stale.
	ErrUpdateUnavailable = errors.New("update_unavailable")
	// ErrChangelogTooLong rejects a changelog above the configured length.
	ErrChangelogTooLong = errors.New("changelog_too_long")
)

// Version is one numbered publication in the owner's version list.
type Version struct {
	Number      int       `json:"number"`
	Verdict     string    `json:"verdict"`
	Live        bool      `json:"live"`
	LessonCount int       `json:"lesson_count"`
	Changelog   string    `json:"changelog"`
	CreatedAt   time.Time `json:"created_at"`
}

// VersionList is the owner's versions with the remaining republish budget.
type VersionList struct {
	Versions             []Version `json:"versions"`
	RepublishesRemaining int       `json:"republishes_remaining"`
	Limit                int       `json:"limit"`
}

// ChangeSummary counts what differs between two versions, matched by lesson key.
type ChangeSummary struct {
	Added    int `json:"added"`
	Removed  int `json:"removed"`
	Retitled int `json:"retitled"`
}

// ChangelogEntry is what a learner sees for one newer version.
type ChangelogEntry struct {
	Version int    `json:"version"`
	Text    string `json:"text"`
	ChangeSummary
}

// UpdateStatus tells a copy's learner whether an update exists.
type UpdateStatus struct {
	UpdateAvailable bool             `json:"update_available"`
	Forked          bool             `json:"forked"`
	CurrentVersion  int              `json:"current_version"`
	LatestVersion   int              `json:"latest_version"`
	LockVersion     int              `json:"lock_version"`
	Changelog       []ChangelogEntry `json:"changelog"`
	Completed       int              `json:"completed_lessons"`
	ArchivedCount   int              `json:"archived_count"`
}

// UpdateResult counts what an update did.
type UpdateResult struct {
	Version     int `json:"version"`
	LockVersion int `json:"lock_version"`
	Kept        int `json:"kept"`
	Added       int `json:"added"`
	Archived    int `json:"archived"`
}

// ArchivedLesson is a lesson removed upstream that stays in the learner's history.
type ArchivedLesson struct {
	Title      string    `json:"title"`
	ArchivedAt time.Time `json:"archived_at"`
	Version    int       `json:"version"`
	Completed  bool      `json:"completed"`
}

// HeldLesson is one live lesson of a copy as the planner sees it.
type HeldLesson struct {
	StubID, Key, Title, Status  string
	Reused, HasContent, HasTest bool
}

// Planned actions for a lesson in the new layout.
const (
	ActionKeep    = "keep"
	ActionReplace = "replace"
	ActionAdd     = "add"
)

// PlannedLesson is one lesson of the copy after the update.
type PlannedLesson struct {
	Key, Title, Goal, StubID, Action, Status string
	Unit, Index                              int
	Source                                   SnapshotLesson
}

// PlannedUnit is one unit after the update.
type PlannedUnit struct {
	Title   string
	Lessons []PlannedLesson
}

// UpdatePlan is the pure description of an update.
type UpdatePlan struct {
	Units                 []PlannedUnit
	Archive               []string
	Kept, Added, Archived int
}

// LessonKey is the deterministic stable key of an owner lesson stub: the first 32 hex characters
// of sha256("lesson-key:"+id). The SQL in insertDraft computes the same value.
func LessonKey(stubID string) string {
	sum := sha256.Sum256([]byte("lesson-key:" + stubID))
	return hex.EncodeToString(sum[:])[:32]
}

// KeyOrLegacy returns a snapshot lesson's key, or a position-derived one for pre-E15-05 versions.
func KeyOrLegacy(l SnapshotLesson, unit, lesson int) string {
	if l.Key != "" {
		return l.Key
	}
	return fmt.Sprintf("legacy-%d-%d", unit, lesson)
}

type keyedLesson struct {
	key, title string
	lesson     SnapshotLesson
}

// flatten lists a snapshot's lessons in order with their keys.
func flatten(snap Snapshot) []keyedLesson {
	var out []keyedLesson
	for ui, u := range snap.Units {
		for li, l := range u.Lessons {
			out = append(out, keyedLesson{KeyOrLegacy(l, ui, li), l.Title, l})
		}
	}
	return out
}

// DiffSnapshots counts additions, removals and retitles between two versions by lesson key.
func DiffSnapshots(oldSnap, newSnap Snapshot) ChangeSummary {
	before := map[string]string{}
	for _, l := range flatten(oldSnap) {
		before[l.key] = l.title
	}
	var out ChangeSummary
	seen := map[string]bool{}
	for _, l := range flatten(newSnap) {
		seen[l.key] = true
		title, ok := before[l.key]
		switch {
		case !ok:
			out.Added++
		case strings.TrimSpace(title) != strings.TrimSpace(l.title):
			out.Retitled++
		}
	}
	for key := range before {
		if !seen[key] {
			out.Removed++
		}
	}
	return out
}

// PlanUpdate maps a copy's lessons onto the new version by key. A held lesson without a key (a copy
// made before keys existed) is matched by an unambiguous title. Completed, tested and learner-generated
// lessons keep their content; untouched reused lessons take the new version's. Only the first
// unfinished lesson is open afterwards; an in-flight generation is never touched.
func PlanUpdate(have []HeldLesson, next Snapshot, origin string) UpdatePlan {
	wanted := flatten(next)
	byTitle := map[string][]int{}
	byKey := map[string]int{}
	for i, w := range wanted {
		byKey[w.key] = i
		byTitle[strings.ToLower(strings.TrimSpace(w.title))] = append(byTitle[strings.ToLower(strings.TrimSpace(w.title))], i)
	}
	claimed := map[int]HeldLesson{}
	var archive []string
	for _, h := range have {
		idx, ok := byKey[h.Key]
		if h.Key == "" {
			ok = false
			if c := byTitle[strings.ToLower(strings.TrimSpace(h.Title))]; len(c) == 1 {
				idx, ok = c[0], true
			}
		}
		if _, taken := claimed[idx]; ok && taken {
			ok = false
		}
		if !ok {
			archive = append(archive, h.StubID)
			continue
		}
		claimed[idx] = h
	}
	plan := UpdatePlan{Archive: archive, Archived: len(archive)}
	opened := false
	idx := 0
	for ui, u := range next.Units {
		pu := PlannedUnit{Title: u.Title}
		for li, l := range u.Lessons {
			w := wanted[idx]
			h, held := claimed[idx]
			idx++
			pl := PlannedLesson{Key: w.key, Title: strings.TrimSpace(l.Title), Goal: strings.TrimSpace(l.Goal), Source: l, Unit: ui, Index: li}
			ready := Reusable(l, origin)
			hasContent := ready
			switch {
			case !held:
				pl.Action = ActionAdd
				plan.Added++
			case h.Status == "done" || h.Status == "generating" || h.HasTest || (h.HasContent && !h.Reused):
				pl.Action, pl.StubID, hasContent = ActionKeep, h.StubID, h.HasContent
				plan.Kept++
			default:
				pl.Action, pl.StubID = ActionReplace, h.StubID
				plan.Kept++
			}
			switch {
			case held && h.Status == "done":
				pl.Status = "done"
			case held && h.Status == "generating":
				pl.Status, opened = "generating", true
			case !opened:
				opened = true
				pl.Status = "ready_to_generate"
				if hasContent {
					pl.Status = "current"
				}
			default:
				pl.Status = "locked"
			}
			pu.Lessons = append(pu.Lessons, pl)
		}
		plan.Units = append(plan.Units, pu)
	}
	return plan
}

// CleanChangelog drops control characters, trims the owner text and rejects one above limit characters.
func CleanChangelog(text string, limit int) (string, error) {
	text = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			return -1
		}
		return r
	}, text))
	if utf8.RuneCountInString(text) > limit {
		return "", ErrChangelogTooLong
	}
	return text, nil
}

// ChangelogLimit is the configured owner changelog length.
func (s *Service) ChangelogLimit() int { return s.cfg.MaxChangelogLen }

// livePassDraft is the latest passing version of a course: the one every new enrollment, preview and
// listing serves.
const livePassSQL = //nolint:gosec // SQL text, not a credential
`SELECT id::text, number, content_hash, snapshot, lesson_count FROM publication_drafts WHERE course_id=$1 AND verdict='pass' ORDER BY number DESC LIMIT 1`

// Republish builds version n+1 through the E15-01 draft and screening path. Only a passing version
// becomes live; the previous live version keeps serving until then.
func (s *Service) Republish(ctx context.Context, owner, courseID, changelog string) (Draft, error) {
	if !validID(owner) || !validID(courseID) {
		return Draft{}, ErrUnavailable
	}
	text, err := CleanChangelog(changelog, s.cfg.MaxChangelogLen)
	if err != nil {
		return Draft{}, err
	}
	adult := false
	if s.age != nil {
		if adult, err = s.age.MeetsMinAge(ctx, owner, s.cfg.MinPublisherAge); err != nil {
			return Draft{}, err
		}
	}
	if !adult {
		return Draft{}, ErrAdultRequired
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Draft{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = owns(ctx, tx, owner, courseID); err != nil {
		return Draft{}, err
	}
	if err = s.moderated(ctx, tx, owner, courseID); err != nil {
		return Draft{}, err
	}
	// Serialized by the owner's course row lock, so concurrent republishes cannot exceed the cap.
	var published bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM publication_drafts WHERE course_id=$1 AND verdict='pass')
AND (SELECT visibility<>'private' FROM courses WHERE id=$1)`, courseID).Scan(&published); err != nil {
		return Draft{}, err
	}
	if !published {
		return Draft{}, ErrNotPublished
	}
	var used int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM publication_drafts WHERE course_id=$1 AND republish AND created_at > now() - interval '24 hours'`, courseID).Scan(&used); err != nil {
		return Draft{}, err
	}
	if used >= s.cfg.MaxRepublishesPerDay {
		return Draft{}, ErrRepublishLimit
	}
	id, snap, findings, err := s.insertDraft(ctx, tx, owner, courseID, text, true)
	if err != nil {
		return Draft{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Draft{}, err
	}
	s.runScreen(ctx, owner, id, snap, findings)
	got, err := s.Get(ctx, owner, courseID, id)
	if err != nil {
		return Draft{}, err
	}
	if got.Verdict == VerdictPass {
		s.followLatestPass(ctx, courseID)
	}
	s.emit(owner, "course_republished", map[string]any{"course_id": courseID, "version": got.Number, "lesson_count": got.LessonCount, "verdict": got.Verdict})
	return got, nil
}

// followLatestPass keeps a live Marketplace listing pointing at the latest passing version.
func (s *Service) followLatestPass(ctx context.Context, courseID string) {
	var id, hash string
	var number, lessons int
	var raw []byte
	if err := s.pool.QueryRow(ctx, livePassSQL, courseID).Scan(&id, &number, &hash, &raw, &lessons); err != nil {
		return
	}
	var snap Snapshot
	if json.Unmarshal(raw, &snap) != nil || strings.TrimSpace(snap.Title) == "" || strings.TrimSpace(snap.Description) == "" {
		return
	}
	generated := 0
	for _, l := range flatten(snap) {
		if l.lesson.Generated {
			generated++
		}
	}
	tag, err := s.pool.Exec(ctx, `UPDATE marketplace_listings SET draft_id=$2,version=$3,title=$4,description=$5,lesson_count=$6,generated_lessons=$7,
  search=setweight(to_tsvector('simple',$4),'A') || setweight(to_tsvector('simple',array_to_string(tags,' ')),'B') || setweight(to_tsvector('simple',$5),'C')
WHERE course_id=$1 AND delisted_at IS NULL AND version<$3`, courseID, id, number, snap.Title, snap.Description, lessons, generated)
	if err != nil {
		slog.WarnContext(ctx, "marketplace listing did not follow the republish", "error", err)
		return
	}
	if tag.RowsAffected() > 0 {
		s.invalidateCatalogue(ctx)
	}
}

// Versions lists the owner's versions and the remaining republish budget.
func (s *Service) Versions(ctx context.Context, owner, courseID string) (VersionList, error) {
	if !validID(owner) || !validID(courseID) {
		return VersionList{}, ErrUnavailable
	}
	var ok bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM courses WHERE id=$1 AND learner_id=$2 AND deleted_at IS NULL)`, courseID, owner).Scan(&ok); err != nil {
		return VersionList{}, err
	}
	if !ok {
		return VersionList{}, ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, `SELECT number,verdict,lesson_count,changelog,created_at,
  verdict='pass' AND number=(SELECT max(p.number) FROM publication_drafts p WHERE p.course_id=$1 AND p.verdict='pass')
FROM publication_drafts WHERE course_id=$1 ORDER BY number DESC LIMIT 100`, courseID)
	if err != nil {
		return VersionList{}, err
	}
	defer rows.Close()
	out := VersionList{Versions: []Version{}, Limit: s.cfg.MaxRepublishesPerDay}
	for rows.Next() {
		var v Version
		if err = rows.Scan(&v.Number, &v.Verdict, &v.LessonCount, &v.Changelog, &v.CreatedAt, &v.Live); err != nil {
			return VersionList{}, err
		}
		out.Versions = append(out.Versions, v)
	}
	if err = rows.Err(); err != nil {
		return VersionList{}, err
	}
	var used int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM publication_drafts WHERE course_id=$1 AND republish AND created_at > now() - interval '24 hours'`, courseID).Scan(&used); err != nil {
		return VersionList{}, err
	}
	out.RepublishesRemaining = max(0, s.cfg.MaxRepublishesPerDay-used)
	return out, nil
}

// pin is a learner's copy pin as read for an update.
type pin struct {
	source, draft string
	version, lock int
	forked        bool
	hasSource     bool
}

func readPin(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, learner, copyID string, lock bool) (pin, error) {
	var p pin
	var source, draft *string
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF cc"
	}
	err := q.QueryRow(ctx, `SELECT cc.source_course_id::text, cc.draft_id::text, cc.version, cc.lock_version, cc.forked_at IS NOT NULL
FROM course_copies cc JOIN courses c ON c.id=cc.copy_course_id WHERE cc.copy_course_id=$1 AND cc.learner_id=$2 AND c.deleted_at IS NULL`+suffix, copyID, learner).
		Scan(&source, &draft, &p.version, &p.lock, &p.forked)
	if errors.Is(err, pgx.ErrNoRows) {
		return pin{}, ErrUpdateUnavailable
	}
	if err != nil {
		return pin{}, err
	}
	if source != nil {
		p.source, p.hasSource = *source, true
	}
	if draft != nil {
		p.draft = *draft
	}
	return p, nil
}

// UpdateStatus tells a copy's learner whether a newer passing version exists, with its changelog.
// A forked copy never sees one. Courses the learner owns that are not copies report no update.
func (s *Service) UpdateStatus(ctx context.Context, learner, copyID string) (UpdateStatus, error) {
	st := UpdateStatus{Changelog: []ChangelogEntry{}}
	if !validID(learner) || !validID(copyID) {
		return st, ErrUpdateUnavailable
	}
	p, err := readPin(ctx, s.pool, learner, copyID, false)
	if errors.Is(err, ErrUpdateUnavailable) {
		var own bool
		if qerr := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM courses WHERE id=$1 AND learner_id=$2 AND deleted_at IS NULL)
AND NOT EXISTS(SELECT 1 FROM course_copies WHERE copy_course_id=$1)`, copyID, learner).Scan(&own); qerr == nil && own {
			return st, nil
		}
		return st, ErrUpdateUnavailable
	}
	if err != nil {
		return st, err
	}
	st.CurrentVersion, st.LatestVersion, st.LockVersion, st.Forked = p.version, p.version, p.lock, p.forked
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE st.status='done' AND u.archived_at IS NULL), count(*) FILTER (WHERE u.archived_at IS NOT NULL)
FROM course_lesson_stubs st JOIN course_units u ON u.id=st.unit_id WHERE u.course_id=$1`, copyID).Scan(&st.Completed, &st.ArchivedCount); err != nil {
		return st, err
	}
	if p.forked || !p.hasSource {
		return st, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT number,changelog,snapshot FROM publication_drafts WHERE course_id=$1 AND verdict='pass' AND number>=$2 ORDER BY number`, p.source, p.version)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	var base *Snapshot // the previous passing version, starting at the pinned one
	for rows.Next() {
		var number int
		var text string
		var raw []byte
		if err = rows.Scan(&number, &text, &raw); err != nil {
			return st, err
		}
		var snap Snapshot
		if err = json.Unmarshal(raw, &snap); err != nil {
			return st, err
		}
		if number > p.version {
			entry := ChangelogEntry{Version: number, Text: text}
			if base != nil {
				entry.ChangeSummary = DiffSnapshots(*base, snap)
			}
			st.Changelog = append(st.Changelog, entry)
			st.LatestVersion = number
		}
		base = &snap
	}
	if err = rows.Err(); err != nil {
		return st, err
	}
	st.UpdateAvailable = st.LatestVersion > st.CurrentVersion
	if len(st.Changelog) > 10 {
		st.Changelog = st.Changelog[len(st.Changelog)-10:]
	}
	return st, nil
}

const archiveUnitPosition = 1000000

// ApplyUpdate re-pins a copy to the latest passing version. The pin row is locked and its
// lock_version must match, so concurrent or stale updates are rejected without detail.
func (s *Service) ApplyUpdate(ctx context.Context, learner, copyID string, lockVersion, toVersion int) (UpdateResult, error) {
	if !validID(learner) || !validID(copyID) {
		return UpdateResult{}, ErrUpdateUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UpdateResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	p, err := readPin(ctx, tx, learner, copyID, true)
	if err != nil {
		return UpdateResult{}, err
	}
	if p.forked || !p.hasSource || p.lock != lockVersion || toVersion <= p.version {
		return UpdateResult{}, ErrUpdateUnavailable
	}
	var draftID, hash string
	var number, lessons int
	var raw []byte
	if err = tx.QueryRow(ctx, livePassSQL, p.source).Scan(&draftID, &number, &hash, &raw, &lessons); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UpdateResult{}, ErrUpdateUnavailable
		}
		return UpdateResult{}, err
	}
	if number != toVersion {
		return UpdateResult{}, ErrUpdateUnavailable
	}
	var snap Snapshot
	if err = json.Unmarshal(raw, &snap); err != nil {
		return UpdateResult{}, err
	}
	have, err := s.heldLessons(ctx, tx, copyID)
	if err != nil {
		return UpdateResult{}, err
	}
	plan := PlanUpdate(have, snap, s.assetOrigin)
	if err = s.applyPlan(ctx, tx, learner, copyID, draftID, number, plan); err != nil {
		return UpdateResult{}, err
	}
	var lock int
	if err = tx.QueryRow(ctx, `UPDATE course_copies SET draft_id=$2,version=$3,content_hash=$4,lock_version=lock_version+1,updated_at=now() WHERE copy_course_id=$1 RETURNING lock_version`,
		copyID, draftID, number, hash).Scan(&lock); err != nil {
		return UpdateResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return UpdateResult{}, err
	}
	s.refreshListingStats(ctx, p.source)
	s.emit(learner, "course_update_applied", map[string]any{"course_id": copyID, "source_course_id": p.source, "from_version": p.version, "to_version": number,
		"kept": plan.Kept, "added": plan.Added, "archived": plan.Archived})
	return UpdateResult{Version: number, LockVersion: lock, Kept: plan.Kept, Added: plan.Added, Archived: plan.Archived}, nil
}

func (s *Service) heldLessons(ctx context.Context, tx pgx.Tx, copyID string) ([]HeldLesson, error) {
	rows, err := tx.Query(ctx, `SELECT st.id::text, COALESCE(st.lesson_key,''), st.title, st.status, st.reused_from_snapshot,
  EXISTS(SELECT 1 FROM generated_lessons g WHERE g.lesson_stub_id=st.id),
  EXISTS(SELECT 1 FROM lesson_test_sessions t WHERE t.lesson_stub_id=st.id)
FROM course_lesson_stubs st JOIN course_units u ON u.id=st.unit_id WHERE u.course_id=$1 AND u.archived_at IS NULL ORDER BY u.position, st.position FOR UPDATE OF st`, copyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HeldLesson
	for rows.Next() {
		var h HeldLesson
		if err = rows.Scan(&h.StubID, &h.Key, &h.Title, &h.Status, &h.Reused, &h.HasContent, &h.HasTest); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// applyPlan rewrites the copy's outline in place. Positions and authored ids go through a temporary
// range so the unique constraints hold at every step.
func (s *Service) applyPlan(ctx context.Context, tx pgx.Tx, learner, copyID, draftID string, version int, plan UpdatePlan) error {
	exec := func(q string, args ...any) error { _, err := tx.Exec(ctx, q, args...); return err }
	if err := pinPrefix(ctx, tx, draftID); err != nil {
		return err
	}
	if err := exec(`UPDATE course_units SET position=position+500000 WHERE course_id=$1 AND archived_at IS NULL`, copyID); err != nil {
		return err
	}
	if err := exec(`UPDATE course_lesson_stubs st SET position=st.position+500000, authored_id='tmp-'||st.id::text FROM course_units u WHERE u.id=st.unit_id AND u.course_id=$1 AND u.archived_at IS NULL`, copyID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id::text FROM course_units WHERE course_id=$1 AND archived_at IS NULL ORDER BY position`, copyID)
	if err != nil {
		return err
	}
	var existing []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	// Archive removed lessons into the copy's hidden unit; nothing is deleted.
	if len(plan.Archive) > 0 {
		var archiveUnit string
		err = tx.QueryRow(ctx, `SELECT id::text FROM course_units WHERE course_id=$1 AND archived_at IS NOT NULL`, copyID).Scan(&archiveUnit)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `INSERT INTO course_units(learner_id,course_id,authored_id,position,title,archived_at) VALUES($1,$2,'archived',$3,'Archived lessons',now()) RETURNING id::text`, learner, copyID, archiveUnitPosition).Scan(&archiveUnit)
		}
		if err != nil {
			return err
		}
		var next int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max(position),-1)+1 FROM course_lesson_stubs WHERE unit_id=$1`, archiveUnit).Scan(&next); err != nil {
			return err
		}
		for i, stub := range plan.Archive {
			if err = exec(`UPDATE course_lesson_stubs SET unit_id=$2, position=$3, authored_id='a-'||id::text, archived_at=now(), archived_from_version=$4 WHERE id=$1`, stub, archiveUnit, next+i, version); err != nil {
				return err
			}
		}
	}
	for ui, u := range plan.Units {
		var unitID string
		if ui < len(existing) {
			unitID = existing[ui]
			if err = exec(`UPDATE course_units SET position=$2, title=$3, authored_id=$4 WHERE id=$1`, unitID, ui, u.Title, fmt.Sprintf("tmp-u%d", ui+1)); err != nil {
				return err
			}
		} else if err = tx.QueryRow(ctx, `INSERT INTO course_units(learner_id,course_id,authored_id,position,title) VALUES($1,$2,$3,$4,$5) RETURNING id::text`, learner, copyID, fmt.Sprintf("tmp-u%d", ui+1), ui, u.Title).Scan(&unitID); err != nil {
			return err
		}
		for li, l := range u.Lessons {
			prefix := fmt.Sprintf("lessons/published/%s/%d-%d", draftID, ui, li)
			ready := Reusable(l.Source, s.assetOrigin)
			stub := l.StubID
			switch l.Action {
			case ActionAdd:
				if err = tx.QueryRow(ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status,reused_from_snapshot,lesson_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id::text`,
					learner, unitID, fmt.Sprintf("l%d", li+1), li, l.Title, l.Goal, l.Status, ready, l.Key).Scan(&stub); err != nil {
					return err
				}
				if ready {
					if err = s.copyLesson(ctx, tx, learner, copyID, stub, prefix, version, l.Source); err != nil {
						return err
					}
				}
				continue
			case ActionReplace:
				for _, q := range []string{`DELETE FROM lesson_manifests WHERE lesson_id=$1`, `DELETE FROM lesson_slides WHERE lesson_stub_id=$1`, `DELETE FROM lesson_questions WHERE lesson_stub_id=$1`,
					`DELETE FROM lesson_retry_pools WHERE lesson_stub_id=$1`, `DELETE FROM generated_lessons WHERE lesson_stub_id=$1`} {
					if err = exec(q, stub); err != nil {
						return err
					}
				}
				if ready {
					if err = s.copyLesson(ctx, tx, learner, copyID, stub, prefix, version, l.Source); err != nil {
						return err
					}
				}
				if err = exec(`UPDATE course_lesson_stubs SET reused_from_snapshot=$2 WHERE id=$1`, stub, ready); err != nil {
					return err
				}
			}
			if err = exec(`UPDATE course_lesson_stubs SET unit_id=$2, position=$3, authored_id=$4, title=$5, goal=$6, status=$7, lesson_key=$8 WHERE id=$1`,
				stub, unitID, li, fmt.Sprintf("l%d", li+1), l.Title, l.Goal, l.Status, l.Key); err != nil {
				return err
			}
		}
	}
	// Units beyond the new outline are now empty; drop them and settle the unit ids.
	if len(existing) > len(plan.Units) {
		if err = exec(`DELETE FROM course_units WHERE id = ANY($1::uuid[])`, existing[len(plan.Units):]); err != nil {
			return err
		}
	}
	for ui := range plan.Units {
		if err = exec(`UPDATE course_units SET authored_id=$2 WHERE course_id=$1 AND position=$3 AND archived_at IS NULL`, copyID, fmt.Sprintf("u%d", ui+1), ui); err != nil {
			return err
		}
	}
	return nil
}

// ArchivedLessons is the learner's history of lessons removed upstream.
func (s *Service) ArchivedLessons(ctx context.Context, learner, copyID string) ([]ArchivedLesson, error) {
	if !validID(learner) || !validID(copyID) {
		return nil, ErrUpdateUnavailable
	}
	if _, err := readPin(ctx, s.pool, learner, copyID, false); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT st.title, st.archived_at, COALESCE(st.archived_from_version,0), st.status='done'
FROM course_lesson_stubs st JOIN course_units u ON u.id=st.unit_id WHERE u.course_id=$1 AND u.archived_at IS NOT NULL AND st.archived_at IS NOT NULL ORDER BY st.archived_at, st.position`, copyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ArchivedLesson{}
	for rows.Next() {
		var a ArchivedLesson
		if err = rows.Scan(&a.Title, &a.ArchivedAt, &a.Version, &a.Completed); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RetainedObjectPrefixes lists the published-object prefixes still needed: every version a copy pins
// and every version a copy lesson manifest points into. Nothing may be purged under these.
func (s *Service) RetainedObjectPrefixes(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT p FROM (
  SELECT 'lessons/published/'||draft_id::text||'/' AS p FROM course_copies WHERE draft_id IS NOT NULL
  UNION ALL
  SELECT substring(manifest_key from '^lessons/published/[0-9a-f-]{36}/') FROM lesson_manifests WHERE manifest_key LIKE 'lessons/published/%'
) x WHERE p IS NOT NULL ORDER BY p`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err = rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Updater is what the update routes need from the service.
type Updater interface {
	Republish(ctx context.Context, owner, courseID, changelog string) (Draft, error)
	Versions(ctx context.Context, owner, courseID string) (VersionList, error)
	UpdateStatus(ctx context.Context, learner, copyID string) (UpdateStatus, error)
	ApplyUpdate(ctx context.Context, learner, copyID string, lockVersion, toVersion int) (UpdateResult, error)
	ArchivedLessons(ctx context.Context, learner, copyID string) ([]ArchivedLesson, error)
}

// UpdateHandler serves the republish and copy update routes; the router protects them, so the
// actor always comes from the session.
type UpdateHandler struct{ u Updater }

// NewUpdateHandler wires the routes.
func NewUpdateHandler(u Updater) *UpdateHandler { return &UpdateHandler{u: u} }

// UpdatePatterns are the routes the update handler serves.
var UpdatePatterns = []string{
	"POST /v1/courses/{id}/republish", "GET /v1/courses/{id}/versions",
	"GET /v1/courses/{id}/update", "POST /v1/courses/{id}/update", "GET /v1/courses/{id}/archived-lessons",
}

func (h *UpdateHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	code := func(status int, c string) { write(w, status, map[string]string{"error": c}) }
	switch {
	case errors.Is(err, ErrUnavailable):
		code(http.StatusNotFound, "draft_unavailable")
	case errors.Is(err, ErrAdultRequired):
		code(http.StatusForbidden, "adult_required")
	case errors.Is(err, ErrNotPublished):
		code(http.StatusConflict, "not_published")
	case errors.Is(err, ErrRepublishLimit):
		w.Header().Set("Retry-After", "86400")
		code(http.StatusTooManyRequests, "republish_limit")
	case errors.Is(err, ErrChangelogTooLong):
		code(http.StatusUnprocessableEntity, "changelog_too_long")
	case errors.Is(err, ErrUpdateUnavailable):
		code(http.StatusConflict, "update_unavailable")
	default:
		slog.ErrorContext(r.Context(), "republish request failed", "error", err)
		code(http.StatusServiceUnavailable, "sharing_unavailable")
	}
}

func (h *UpdateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor := accounts.LearnerID(r.Context())
	if actor == "" {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	id := r.PathValue("id")
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/republish"):
		var body struct {
			Changelog string `json:"changelog"`
		}
		if !decodeBody(w, r, &body, true) {
			return
		}
		limit := 500
		if l, ok := h.u.(interface{ ChangelogLimit() int }); ok {
			limit = l.ChangelogLimit()
		}
		text, err := CleanChangelog(body.Changelog, limit)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		d, err := h.u.Republish(r.Context(), actor, id, text)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		write(w, http.StatusCreated, d)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/versions"):
		v, err := h.u.Versions(r.Context(), actor, id)
		h.reply(w, r, v, err)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/update"):
		st, err := h.u.UpdateStatus(r.Context(), actor, id)
		h.reply(w, r, st, err)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/update"):
		var body struct {
			Lock int `json:"lock_version"`
			To   int `json:"to_version"`
		}
		if !decodeBody(w, r, &body, false) {
			return
		}
		if body.Lock <= 0 || body.To <= 0 {
			write(w, http.StatusBadRequest, map[string]string{"error": "bad_request"})
			return
		}
		res, err := h.u.ApplyUpdate(r.Context(), actor, id, body.Lock, body.To)
		h.reply(w, r, res, err)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/archived-lessons"):
		l, err := h.u.ArchivedLessons(r.Context(), actor, id)
		h.reply(w, r, map[string]any{"lessons": l}, err)
	default:
		http.NotFound(w, r)
	}
}

func (h *UpdateHandler) reply(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	write(w, http.StatusOK, v)
}

// decodeBody reads a small JSON body; an empty body is accepted only when optional.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any, optional bool) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<10))
	if err == nil && len(strings.TrimSpace(string(raw))) == 0 && optional {
		return true
	}
	if err == nil {
		err = json.Unmarshal(raw, dst)
	}
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "bad_request"})
		return false
	}
	return true
}
