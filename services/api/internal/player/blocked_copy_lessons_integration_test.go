//go:build integration

package player_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
)

// A copy blocked by a severe takedown (E15-10) refuses its lessons; every other copy plays as before.
func TestBlockedCopyRefusesLessonsAndKeepsOtherCopiesPlaying(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	mail := new(accounts.MailSink)
	auth, err := accounts.New(f.DB.Pool, strings.Repeat("player-test", 4), mail)
	if err != nil {
		t.Fatal(err)
	}
	service := player.New(player.Options{Pool: f.DB.Pool, Objects: f.ApplicationObjects(), AssetOrigin: "https://assets.example.test"})
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Player: service}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	call := func(method, path, token string, body any) (int, []byte) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequestWithContext(t.Context(), method, server.URL+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = resp.Body.Close() }()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}
	email := "synthetic@example.test"
	if c, b := call("POST", "/v1/auth/code", "", map[string]any{"email": email, "age_confirmed": true}); c != 202 {
		t.Fatal(c, string(b))
	}
	c, b := call("POST", "/v1/auth/verify", "", map[string]any{"email": email, "code": mail.Code(email), "age_confirmed": true})
	var token struct {
		Access string `json:"access_token"`
	}
	if err = json.Unmarshal(b, &token); err != nil || c != 200 {
		t.Fatal(c, string(b), err)
	}
	const lesson = "f3333333-3333-4333-8333-333333333333"
	path := "/v1/lessons/" + lesson + "/manifest"
	if c, b = call("GET", path, token.Access, nil); c != 200 {
		t.Fatalf("before the copy is blocked: %d %s", c, b)
	}
	var course, learner string
	if err = f.DB.Pool.QueryRow(t.Context(), `SELECT u.course_id::text,l.learner_id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE l.id=$1`, lesson).Scan(&course, &learner); err != nil {
		t.Fatal(err)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO course_copies(copy_course_id,learner_id,version,content_hash) VALUES($1,$2,1,repeat('a',64))`, course, learner); err != nil {
		t.Fatal(err)
	}
	if c, b = call("GET", path, token.Access, nil); c != 200 {
		t.Fatalf("an ordinary copy must keep playing: %d %s", c, b)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `UPDATE course_copies SET blocked_at=now() WHERE copy_course_id=$1`, course); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, "/v1/lessons/" + lesson + "/position"} {
		if c, b = call("GET", p, token.Access, nil); c != 404 {
			t.Fatalf("blocked copy served %s: %d %s", p, c, b)
		}
	}
}

// The offline download route sits behind the subscription entitlement, so it needs its own proof:
// an entitled subscriber downloads an ordinary copy, is refused once a severe takedown blocks it, and
// downloads again after the block is lifted.
func TestBlockedCopyCannotBeDownloadedForOffline(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t) // the seeded learner holds the funded plan with the offline download benefit
	mail := new(accounts.MailSink)
	auth, err := accounts.New(f.DB.Pool, strings.Repeat("player-test", 4), mail)
	if err != nil {
		t.Fatal(err)
	}
	catalogue, err := pricing.Load(t.Context(), f.DB.Queries)
	if err != nil {
		t.Fatal(err)
	}
	creditService := credits.NewServiceWithCatalogue(f.DB.Pool, nil, catalogue, nil)
	service := player.New(player.Options{Pool: f.DB.Pool, Objects: f.ApplicationObjects(), AssetOrigin: "https://assets.example.test", Credits: creditService})
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Player: service, Credits: creditService}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	call := func(method, path, token string, body any) (int, []byte) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequestWithContext(t.Context(), method, server.URL+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = resp.Body.Close() }()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}
	email := "synthetic@example.test"
	if c, b := call("POST", "/v1/auth/code", "", map[string]any{"email": email, "age_confirmed": true}); c != 202 {
		t.Fatal(c, string(b))
	}
	c, b := call("POST", "/v1/auth/verify", "", map[string]any{"email": email, "code": mail.Code(email), "age_confirmed": true})
	var token struct {
		Access string `json:"access_token"`
	}
	if err = json.Unmarshal(b, &token); err != nil || c != 200 {
		t.Fatal(c, string(b), err)
	}
	const course, learner = "f1111111-1111-4111-8111-111111111111", "a1111111-1111-4111-8111-111111111111"
	path := "/v1/courses/" + course + "/download"
	if c, b = call("GET", path, token.Access, nil); c != 200 {
		t.Fatalf("entitled download before any block: %d %s", c, b)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `INSERT INTO course_copies(copy_course_id,learner_id,version,content_hash) VALUES($1,$2,1,repeat('a',64))`, course, learner); err != nil {
		t.Fatal(err)
	}
	if c, b = call("GET", path, token.Access, nil); c != 200 {
		t.Fatalf("an ordinary copy must stay downloadable: %d %s", c, b)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `UPDATE course_copies SET blocked_at=now() WHERE copy_course_id=$1`, course); err != nil {
		t.Fatal(err)
	}
	if c, b = call("GET", path, token.Access, nil); c != 404 || strings.Contains(string(b), "lessons") {
		t.Fatalf("blocked copy was offered for download: %d %s", c, b)
	}
	if _, err = f.DB.Pool.Exec(t.Context(), `UPDATE course_copies SET blocked_at=NULL WHERE copy_course_id=$1`, course); err != nil {
		t.Fatal(err)
	}
	if c, b = call("GET", path, token.Access, nil); c != 200 {
		t.Fatalf("an unblocked copy must download again: %d %s", c, b)
	}
}
