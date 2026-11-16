package accounts

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5/pgtype"
)

var errInvalidBirthDate = errors.New("invalid_birth_date")

// ValidateBirthDate accepts only real ISO calendar dates within the account bounds.
// The clock belongs to the server; callers must never use a client-supplied current date.
func ValidateBirthDate(raw string, now time.Time) (time.Time, error) {
	date, err := time.Parse(time.DateOnly, raw)
	today := now.UTC().Format(time.DateOnly)
	if err != nil || date.Format(time.DateOnly) != raw || raw < "1900-01-01" || raw > today {
		return time.Time{}, errInvalidBirthDate
	}
	return date, nil
}

// MeetsMinimumAge compares calendar birthdays against the server's UTC date.
// February 29 reaches its anniversary on March 1 in non-leap years.
func MeetsMinimumAge(birthDate, now time.Time, minimum int) bool {
	if _, err := ValidateBirthDate(birthDate.Format(time.DateOnly), now); err != nil {
		return false
	}
	now = now.UTC()
	years := now.Year() - birthDate.Year()
	if now.Month() < birthDate.Month() || (now.Month() == birthDate.Month() && now.Day() < birthDate.Day()) {
		years--
	}
	return years >= minimum
}

func (s *Service) saveBirthDate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BirthDate string `json:"birth_date"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	var extra any
	if err := decoder.Decode(&in); err != nil || !errors.Is(decoder.Decode(&extra), io.EOF) {
		respond(w, 400, map[string]string{"error": "invalid_birth_date"})
		return
	}
	date, err := ValidateBirthDate(in.BirthDate, time.Now())
	if err != nil {
		respond(w, 400, map[string]string{"error": "invalid_birth_date"})
		return
	}
	rows, err := db.New(s.pool).SetLearnerBirthDate(r.Context(), db.SetLearnerBirthDateParams{BirthDate: pgtype.Date{Time: date, Valid: true}, LearnerID: contextID(r)})
	if err != nil || rows != 1 {
		respond(w, 503, map[string]string{"error": "birth_date_unavailable"})
		return
	}
	// No analytics or response payload: neither the birth date nor derived age leaves this flow.
	w.WriteHeader(http.StatusNoContent)
}
