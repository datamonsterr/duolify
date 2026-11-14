package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sync"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/datamonsterr/duolify/services/api/internal/narration/narrationtest"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/jackc/pgx/v5/pgxpool"
)

// segmentFaults are speech-twin faults for pinned-speaker dialogue (E17-08), merged into every
// narration queue so a Lesson run never resets them.
var (
	segmentFaultsMu sync.Mutex
	segmentFaults   = map[string]string{}
)

func currentSegmentFaults() map[string]string {
	segmentFaultsMu.Lock()
	defer segmentFaultsMu.Unlock()
	out := map[string]string{}
	for k, v := range segmentFaults {
		out[k] = v
	}
	return out
}

// registerCharacterNarrationRoutes seeds a Lesson whose Slides are spoken by two pinned speakers and
// exposes the voice state the E2E spec asserts. Test servers only.
func registerCharacterNarrationRoutes(mux *http.ServeMux, auth *accounts.Service, pool *pgxpool.Pool, cast *characters.Service, twin *narrationtest.Engine, objects *fixtureObjects) {
	reply := func(w http.ResponseWriter, status int, value any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(value)
	}
	mux.Handle("POST /test/characters/narrated-lesson", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Salt string `json:"salt"`
			// Fault is "engine" to make the supporter's line fail like a provider outage.
			Fault string `json:"fault"`
			// TwinOf copies an earlier seeded Lesson of the Learner so a second Lesson can reuse stored audio.
			TwinOf string `json:"twin_of"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || !regexp.MustCompile(`^[a-z0-9]{4,16}$`).MatchString(in.Salt) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		ctx, owner := r.Context(), accounts.LearnerID(r.Context())
		var lesson string
		var err error
		if in.TwinOf != "" {
			lesson, err = playerfixture.Twin(ctx, pool, owner, in.TwinOf)
		} else {
			lesson, err = playerfixture.Seed(ctx, pool, objects, owner)
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var course string
		if err = pool.QueryRow(ctx, `SELECT c.id::text FROM courses c JOIN course_units u ON u.course_id=c.id JOIN course_lesson_stubs l ON l.unit_id=u.id WHERE l.id=$1`, lesson).Scan(&course); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		key := "e17-08-" + in.Salt
		if _, err = cast.PinCast(ctx, owner, course, key); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if _, err = pool.Exec(ctx, `INSERT INTO generated_lessons(lesson_stub_id,course_id,learner_id,title,manifest) VALUES($1,$2,$3,'Narrated by the Cast',jsonb_build_object('cast_key',$4::text))
			ON CONFLICT (lesson_stub_id) DO UPDATE SET manifest=EXCLUDED.manifest`, lesson, course, owner, key); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		guide, supporter := "Look closely at the sky "+in.Salt+".", "Earth turns every day "+in.Salt+"."
		segment := func(speaker, label, text string, start int) map[string]any {
			return map[string]any{"speaker_key": speaker, "speaker_label": label, "text": text, "expression": "happy", "start_ms": start, "duration_ms": 1000}
		}
		for i, slide := range []struct {
			id       string
			segments []map[string]any
		}{
			{"sky-1", []map[string]any{segment("guide", "Glint", guide, 0), segment("supporter", "Pip", supporter, 1000), segment("guide", "Glint", guide, 2000)}},
			{"sky-2", []map[string]any{segment("supporter", "Pip", supporter, 0)}},
		} {
			raw, _ := json.Marshal(slide.segments)
			if _, err = pool.Exec(ctx, `INSERT INTO lesson_slides(lesson_stub_id,learner_id,position,slide_id,html,duration_ms,narration,segments)
				VALUES($1,$2,$3,$4,'<div id="root"><h1>Sky</h1></div>',3000,'',$5::jsonb)`, lesson, owner, i, slide.id, raw); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		segmentFaultsMu.Lock()
		segmentFaults = map[string]string{}
		if in.Fault != "" {
			segmentFaults[supporter] = in.Fault
		}
		segmentFaultsMu.Unlock()
		reply(w, http.StatusCreated, map[string]string{"lesson_id": lesson, "course_id": course, "guide": guide, "supporter": supporter})
	})))
	// Test servers only: not owner-scoped, like the other global /test switches.
	mux.HandleFunc("POST /test/characters/retire-voices", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Keys []string `json:"keys"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		twin.RetireVoices(in.Keys...)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("POST /test/characters/clear-segment-narration", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			LessonID string `json:"lesson_id"`
			Slide    string `json:"slide_id"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if _, err := pool.Exec(r.Context(), `DELETE FROM slide_narrations WHERE lesson_id::text=$1 AND learner_id::text=$2 AND speaker_key<>'' AND segment_slide_id=$3`, in.LessonID, accounts.LearnerID(r.Context()), in.Slide); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	mux.Handle("GET /test/characters/segments", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := accounts.LearnerID(r.Context())
		rows, err := pool.Query(r.Context(), `SELECT segment_slide_id,segment_index,speaker_key,voice_key,actual_voice_key,voice_config_version,voice_fallback_reason,status,failure_code,left(content_hash,12)
			FROM slide_narrations WHERE lesson_id::text=$1 AND learner_id::text=$2 AND speaker_key<>'' ORDER BY segment_slide_id,segment_index`, r.URL.Query().Get("lesson_id"), owner)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var slide, speaker, requested, actual, version, fallback, status, code, hash string
			var index int
			if err = rows.Scan(&slide, &index, &speaker, &requested, &actual, &version, &fallback, &status, &code, &hash); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			out = append(out, map[string]any{"slide_id": slide, "index": index, "speaker_key": speaker, "requested_voice": requested, "actual_voice": actual, "voice_config_version": version, "fallback_reason": fallback, "status": status, "failure_code": code, "hash": hash})
		}
		var created int
		_ = pool.QueryRow(r.Context(), `SELECT count(*) FROM credit_jobs j JOIN pricing_quotes q ON q.id=j.quote_id WHERE j.learner_id::text=$1 AND q.snapshot->>'action'='character_create'`, owner).Scan(&created)
		reply(w, http.StatusOK, map[string]any{"segments": out, "character_create_jobs": created, "voices_used": twin.VoicesUsed(), "timeouts_ms": twin.SegmentTimeouts(), "note": fmt.Sprint(len(out))})
	})))
}
