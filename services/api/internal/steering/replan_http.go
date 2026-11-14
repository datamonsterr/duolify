package steering

import (
	"encoding/json"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"net/http"
	"strings"
)

func (s *Service) serveReplan(w http.ResponseWriter, r *http.Request) {
	owner, course := accounts.LearnerID(r.Context()), r.PathValue("id")
	var value any
	var err error
	status := http.StatusOK
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/quote") {
		value, err = s.QuoteReplan(r.Context(), owner, course)
	} else {
		var in struct {
			QuoteID string `json:"quote_id"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || in.QuoteID == "" {
			err = refusal("quote_invalid")
		} else {
			var p ReplanProposal
			p, err = s.ProposeReplan(r.Context(), owner, course, in.QuoteID)
			value = p
			status = http.StatusCreated
			if p.State == "running" || p.State == "pending" {
				status = http.StatusAccepted
			}
		}
	}
	if err != nil {
		code := Code(err)
		w.WriteHeader(statusFor(code))
		_ = json.NewEncoder(w).Encode(map[string]string{"reason_code": code})
		return
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
