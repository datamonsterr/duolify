package accounts

import (
	"net/http"
	"time"
)

// deviceSession deliberately excludes email, IP, user-agent and token hashes.
type deviceSession struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Current   bool      `json:"current"`
}

func (s *Service) listSessions(w http.ResponseWriter, r *http.Request) {
	learnerID, _ := r.Context().Value(learnerKey{}).(string)
	currentID, _ := r.Context().Value(sessionKey{}).(string)
	rows, err := s.pool.Query(r.Context(), `SELECT id::text,created_at,expires_at FROM auth_sessions WHERE learner_id=$1::uuid AND revoked_at IS NULL AND expires_at>now() ORDER BY created_at DESC,id`, learnerID)
	if err != nil {
		failure(w, err)
		return
	}
	defer rows.Close()
	sessions := make([]deviceSession, 0)
	for rows.Next() {
		var value deviceSession
		if err = rows.Scan(&value.ID, &value.CreatedAt, &value.ExpiresAt); err != nil {
			failure(w, err)
			return
		}
		value.Current = value.ID == currentID
		sessions = append(sessions, value)
	}
	if err = rows.Err(); err != nil {
		failure(w, err)
		return
	}
	respond(w, http.StatusOK, sessions)
}

func (s *Service) revokeSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionID")
	if !uuidPattern.MatchString(id) {
		respond(w, http.StatusBadRequest, map[string]string{"error": "Invalid session."})
		return
	}
	learnerID, _ := r.Context().Value(learnerKey{}).(string)
	result, err := s.pool.Exec(r.Context(), `UPDATE auth_sessions SET revoked_at=coalesce(revoked_at,now()) WHERE id=$1::uuid AND learner_id=$2::uuid`, id, learnerID)
	if err != nil {
		failure(w, err)
		return
	}
	if result.RowsAffected() == 0 {
		respond(w, http.StatusNotFound, map[string]string{"error": "Session unavailable."})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) revokeSessions(w http.ResponseWriter, r *http.Request) {
	learnerID, _ := r.Context().Value(learnerKey{}).(string)
	_, err := s.pool.Exec(r.Context(), `UPDATE auth_sessions SET revoked_at=coalesce(revoked_at,now()) WHERE learner_id=$1::uuid AND revoked_at IS NULL`, learnerID)
	if err != nil {
		failure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
