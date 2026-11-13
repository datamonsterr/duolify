//go:build integration

package player_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
)

func TestCacheOpenedLessonsAndPreloadSlidesIntegration(t *testing.T) {
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
		out, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, out
	}
	login := func(email string) string {
		if c, b := call("POST", "/v1/auth/code", "", map[string]any{"email": email, "age_confirmed": true}); c != 202 {
			t.Fatal(c, string(b))
		}
		c, b := call("POST", "/v1/auth/verify", "", map[string]any{"email": email, "code": mail.Code(email), "age_confirmed": true})
		if c != 200 {
			t.Fatal(c, string(b))
		}
		var token struct {
			Access string `json:"access_token"`
		}
		if err := json.Unmarshal(b, &token); err != nil {
			t.Fatal(err)
		}
		return token.Access
	}
	owner := login("synthetic@example.test")
	other := login("other-player@example.test")
	const lesson = "f3333333-3333-4333-8333-333333333333"
	path := "/v1/lessons/" + lesson + "/manifest"
	for _, test := range []struct {
		token  string
		status int
	}{{"", 401}, {other, 404}, {owner, 200}} {
		c, b := call("GET", path, test.token, nil)
		if c != test.status {
			t.Fatalf("manifest: %d %s", c, b)
		}
	}
	c, b := call("GET", path, owner, nil)
	var m player.Manifest
	if err := json.Unmarshal(b, &m); err != nil || c != 200 || len(m.Slides) != 3 {
		t.Fatal(c, string(b), err)
	}
	if strings.Contains(string(b), "answer") || strings.Contains(string(b), "html_key") {
		t.Fatal("private storage/assessment leaked", string(b))
	}
	for _, s := range m.Slides {
		var metadata struct {
			Slides []struct {
				SHA string `json:"html_sha256"`
			} `json:"slides"`
		}
		if err := json.Unmarshal(b, &metadata); err != nil {
			t.Fatal(err)
		}
		if len(metadata.Slides[0].SHA) != 64 {
			t.Fatal("missing immutable HTML checksum")
		}
		if s.ID == "" || s.DurationMS <= 0 || s.Assets == nil || s.Checkpoints == nil {
			t.Fatal("incomplete manifest", s)
		}
		if c, _ := call("GET", s.HTMLURL, other, nil); c != 404 {
			t.Fatal("slide authorization", c)
		}
		if c, b := call("GET", s.HTMLURL, owner, nil); c != 200 || !bytes.Contains(b, []byte("html")) {
			t.Fatal(c, string(b))
		}
		_, document := call("GET", s.HTMLURL, owner, nil)
		var body struct {
			HTML string `json:"html"`
		}
		_ = json.Unmarshal(document, &body)
		sum := sha256.Sum256([]byte(body.HTML))
		if s.ID == m.Slides[0].ID && metadata.Slides[0].SHA != hex.EncodeToString(sum[:]) {
			t.Fatal("RustFS bytes do not match manifest checksum")
		}
	}
	// Reads are repeatable and cannot change generation/progress state.
	c, again := call("GET", path, owner, nil)
	if c != 200 || !bytes.Equal(b, again) {
		t.Fatal("unstable manifest")
	}
	var status string
	if err := f.DB.Pool.QueryRow(t.Context(), "SELECT status FROM course_lesson_stubs WHERE id=$1", lesson).Scan(&status); err != nil || status != "ready_to_generate" {
		t.Fatal(status, err)
	}

	// Opaque references are exported; private bodies never enter analytics.
	if c, body := call("GET", "/v1/me/data-export", owner, nil); c != 200 || !bytes.Contains(body, []byte("lesson_manifests")) || !bytes.Contains(body, []byte(playerfixture.ManifestKey)) {
		t.Fatal("missing metadata export", c)
	}
	// Missing/corrupt objects fail closed even for an enrolled owner. Recovery uses
	// protected versions/checksums, with no model or content regeneration.
	objects := f.ApplicationObjects()
	for _, bad := range []struct {
		key      string
		body     string
		endpoint string
	}{
		{playerfixture.ManifestKey, `{"version":1,"answer_key":"private","slides":[]}`, path},
		{"lessons/fixture-v1/one.html", `<html><body><script src="https://evil.test/a.js"></script></body></html>`, m.Slides[0].HTMLURL},
	} {
		if _, err := objects.Client.DeleteObject(t.Context(), &s3.DeleteObjectInput{Bucket: aws.String(objects.Bucket), Key: aws.String(bad.key)}); err != nil {
			t.Fatal(err)
		}
		if c, _ := call("GET", bad.endpoint, owner, nil); c != 503 {
			t.Fatal("missing object readable", c)
		}
		if _, err := objects.Client.PutObject(t.Context(), &s3.PutObjectInput{Bucket: aws.String(objects.Bucket), Key: aws.String(bad.key), Body: strings.NewReader(bad.body)}); err != nil {
			t.Fatal(err)
		}
		if c, body := call("GET", bad.endpoint, owner, nil); c != 503 || bytes.Contains(body, []byte("private")) {
			t.Fatal("corrupt object readable", c)
		}
		f.AssertPlayerObjectsRestored(t)
		if c, _ := call("GET", bad.endpoint, owner, nil); c != 200 {
			t.Fatal("protected restore unreadable", c)
		}
	}
	if _, err := f.DB.Pool.Exec(t.Context(), "UPDATE courses SET deleted_at=now() WHERE id=$1", m.CourseID); err != nil {
		t.Fatal(err)
	}
	if c, _ := call("GET", path, owner, nil); c != 404 {
		t.Fatal("soft deleted lesson readable", c)
	}
	if _, err := f.DB.Pool.Exec(t.Context(), "UPDATE courses SET deleted_at=NULL WHERE id=$1", m.CourseID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.DB.Pool.Exec(t.Context(), "DELETE FROM course_enrollments WHERE course_id=$1", m.CourseID); err != nil {
		t.Fatal(err)
	}
	if c, _ := call("GET", m.Slides[0].HTMLURL, owner, nil); c != 404 {
		t.Fatal("revoked enrollment readable", c)
	}
	if _, err := f.DB.Pool.Exec(t.Context(), "INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1)", accounts.LearnerReference("a1111111-1111-4111-8111-111111111111")); err != nil {
		t.Fatal(err)
	}
	if err := accounts.ApplyDeletionTombstones(t.Context(), f.DB.Pool); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.DB.Pool.QueryRow(t.Context(), "SELECT count(*) FROM lesson_manifests").Scan(&count); err != nil || count != 0 {
		t.Fatal("metadata survived deletion", count, err)
	}
}
