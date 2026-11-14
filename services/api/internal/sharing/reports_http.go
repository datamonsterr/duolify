package sharing

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

// ReportHandler uses only the session identity. Responses never carry reporter or report text.
type ReportHandler struct{ s *Service }

func (h ReportHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	if !h.s.MarketplaceEnabled() {
		marketFail(w, r, 0, ErrMarketplaceDisabled)
		return
	}
	actor := accounts.LearnerID(r.Context())
	if actor == "" {
		write(w, http.StatusUnauthorized, map[string]string{"error": "sign_in_required"})
		return
	}
	var in struct {
		TargetType string `json:"target_type"`
		TargetID   string `json:"target_id"`
		Reason     string `json:"reason"`
		Text       string `json:"text"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(&struct{}{}) != io.EOF {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid_report"})
		return
	}
	res, err := h.s.Report(r.Context(), actor, ReportInput{TargetType: in.TargetType, TargetID: in.TargetID, Reason: in.Reason, Text: in.Text})
	switch {
	case err == nil && res.Merged:
		write(w, http.StatusOK, res)
	case err == nil:
		write(w, http.StatusAccepted, res)
	case errors.Is(err, ErrInvalidReport):
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid_report"})
	case errors.Is(err, ErrSelfReport):
		write(w, http.StatusForbidden, map[string]string{"error": "self_report"})
	default:
		marketFail(w, r, h.s.RetryAfterSeconds(), err)
	}
}
