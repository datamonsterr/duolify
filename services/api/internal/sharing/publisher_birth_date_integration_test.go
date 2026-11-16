//go:build integration

package sharing_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/accountstest"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPublisherBirthDateAccountValidationExportDeletion(t *testing.T) {
	f := newSharingFixture(t)
	mail := new(accounts.MailSink)
	auth, err := accounts.New(f.DB.Pool, strings.Repeat("birth-date-test-key", 3), mail)
	if err != nil {
		t.Fatal(err)
	}
	auth.ConfigureDeletionLedger(accountstest.NewDeletionLedger())
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, r)
		return w
	}
	if w := call("PUT", "/v1/me/birth-date", `{"birth_date":"1990-06-15"}`, ""); w.Code != 401 {
		t.Fatalf("unauthenticated setter=%d", w.Code)
	}
	email := "birth-date@example.test"
	if w := call("POST", "/v1/auth/code", `{"email":"`+email+`","age_confirmed":true}`, ""); w.Code != 202 {
		t.Fatalf("signup=%d %s", w.Code, w.Body)
	}
	w := call("POST", "/v1/auth/verify", `{"email":"`+email+`","code":"`+mail.Code(email)+`","age_confirmed":true}`, "")
	var token accounts.Tokens
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &token) != nil {
		t.Fatalf("signin=%d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{"birth_date":"2023-02-29"}`, `{"birth_date":"1899-12-31"}`, `{"birth_date":"2999-01-01"}`, `{"birth_date":"2000-01-01T00:00:00Z"}`, `{"birth_date":null}`, `{"birth_date":""}`, `{"birth_date":"2000-01-01","age":30}`, `{"birth_date":"2000-01-01"} {}`} {
		if w = call("PUT", "/v1/me/birth-date", body, token.AccessToken); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_birth_date") {
			t.Fatalf("invalid input=%d %s", w.Code, w.Body)
		}
	}
	if w = call("PUT", "/v1/me/birth-date", `{"birth_date":"1990-06-15"}`, token.AccessToken); w.Code != 204 {
		t.Fatalf("setter=%d %s", w.Code, w.Body)
	}
	if w = call("PUT", "/v1/me/birth-date", `{"birth_date":"1990-06-15"}`, token.AccessToken); w.Code != 204 {
		t.Fatalf("identical retry=%d %s", w.Code, w.Body)
	}
	if w = call("PUT", "/v1/me/birth-date", `{"birth_date":"1980-01-01"}`, token.AccessToken); w.Code != 409 || strings.TrimSpace(w.Body.String()) != `{"error":"birth_date_locked"}` {
		t.Fatalf("changed birth date=%d %s", w.Code, w.Body)
	}
	if w = call("GET", "/v1/me/data-export", "", token.AccessToken); w.Code != 200 || !strings.Contains(w.Body.String(), `"birth_date":"1990-06-15"`) {
		t.Fatalf("export=%d %s", w.Code, w.Body)
	}
	if w = call("POST", "/v1/me/deletion-request", `{"confirmation":"DELETE"}`, token.AccessToken); w.Code != 200 || !strings.Contains(w.Body.String(), `"deleted":true`) {
		t.Fatalf("delete=%d %s", w.Code, w.Body)
	}
	var remaining int
	if err = f.DB.Pool.QueryRow(t.Context(), `SELECT count(*) FROM learners WHERE birth_date IS NOT NULL`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("birth dates remain=%d err=%v", remaining, err)
	}
}

func TestPublisherBirthDateConcurrentFirstWrite(t *testing.T) {
	f := newSharingFixture(t)
	ctx := t.Context()
	var id pgtype.UUID
	if err := f.DB.Pool.QueryRow(ctx, `INSERT INTO learners(display_name) VALUES('Concurrent birthday') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	queries := db.New(f.DB.Pool)
	type result struct {
		date string
		rows int64
		err  error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, raw := range []string{"1990-06-15", "2009-06-15"} {
		go func() {
			date, err := time.Parse(time.DateOnly, raw)
			if err != nil {
				results <- result{err: err}
				return
			}
			<-start
			rows, err := queries.SetLearnerBirthDate(ctx, db.SetLearnerBirthDateParams{LearnerID: id, BirthDate: pgtype.Date{Time: date, Valid: true}})
			results <- result{date: raw, rows: rows, err: err}
		}()
	}
	close(start)
	writes := int64(0)
	winner := ""
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.rows < 0 || r.rows > 1 {
			t.Fatalf("affected rows=%d", r.rows)
		}
		writes += r.rows
		if r.rows == 1 {
			winner = r.date
		}
	}
	stored, err := queries.GetLearnerBirthDate(ctx, id)
	if err != nil || writes != 1 || !stored.Valid || stored.Time.Format(time.DateOnly) != winner {
		t.Fatalf("birth date first-write invariant failed: writes=%d err=%v", writes, err)
	}
}

func TestPublisherBirthDateRealAccountAgeGates(t *testing.T) {
	f := newSeededSharingFixture(t)
	ctx := t.Context()
	s := sharing.NewService(f.DB.Pool, nil, sharing.DefaultConfig())
	s.ConfigureScreening(startEngine(t), fixedRates{})
	courseID, _ := seedPublishableCourse(t, f, newService(t, f, startEngine(t), &capture{}))
	// Signup age confirmation and the browser's age are not a publishing age source.
	if _, err := f.DB.Pool.Exec(ctx, `UPDATE learners SET birth_date=NULL WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishLink(ctx, owner, courseID); err == nil || err.Error() != "birth_date_required" {
		t.Fatalf("unknown birth date=%v", err)
	}
	today := time.Now().UTC()
	minor := today.AddDate(-18, 0, 1).Format(time.DateOnly)
	if _, err := f.DB.Pool.Exec(ctx, `UPDATE learners SET birth_date=$2::date WHERE id=$1`, owner, minor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishLink(ctx, owner, courseID); err != sharing.ErrAdultRequired {
		t.Fatalf("minor=%v", err)
	}
	adultDate := today.AddDate(-18, 0, 0).Format(time.DateOnly)
	if _, err := f.DB.Pool.Exec(ctx, `UPDATE learners SET birth_date=$2::date WHERE id=$1`, owner, adultDate); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishLink(ctx, owner, courseID); err != nil {
		t.Fatalf("eighteenth birthday=%v", err)
	}
	if _, err := f.DB.Pool.Exec(ctx, `UPDATE learners SET birth_date=NULL WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RotateLink(ctx, owner, courseID); err == nil || err.Error() != "birth_date_required" {
		t.Fatalf("rotation unknown=%v", err)
	}
	if _, err := s.Republish(ctx, owner, courseID, ""); err == nil || err.Error() != "birth_date_required" {
		t.Fatalf("republish unknown=%v", err)
	}
}
