package main

import (
	"encoding/json"
	"net/http"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Browser setup pins generated fixtures through the real Character service. Production never mounts /test.
func registerCharacterPublicationFixture(mux *http.ServeMux, auth *accounts.Service, pool *pgxpool.Pool, chars *characters.Service) {
	mux.Handle("POST /test/characters/publication-cast", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Course string `json:"course_id"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in) != nil {
			w.WriteHeader(400)
			return
		}
		owner := accounts.LearnerID(r.Context())
		if _, err := chars.PinCast(r.Context(), owner, in.Course, "browser-publication-fixture"); err != nil {
			w.WriteHeader(404)
			return
		}
		if _, err := pool.Exec(r.Context(), `UPDATE generated_lessons SET manifest=manifest||'{"cast_key":"browser-publication-fixture"}'::jsonb WHERE course_id=$1 AND learner_id=$2`, in.Course, owner); err != nil {
			w.WriteHeader(500)
			return
		}
		if _, err := pool.Exec(r.Context(), `UPDATE lesson_slides s SET segments='[{"speaker_key":"guide","speaker_label":"Seed guide","text":"Observe the Moon.","expression":"happy","start_ms":0,"duration_ms":1000}]'::jsonb FROM generated_lessons g WHERE s.lesson_stub_id=g.lesson_stub_id AND g.course_id=$1 AND g.learner_id=$2`, in.Course, owner); err != nil {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))
}
