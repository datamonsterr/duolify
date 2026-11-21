package e2e

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/objecttest"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
)

// E18-06: while objects move to the new store, a Lesson whose Slides exist only in the old
// bucket still plays when the temporary read-fallback is on, and not when it is off.
func TestLessonPlaysFromOldBucketWhenReadFallbackIsOn(t *testing.T) {
	auth, token, db := signedInDatabase(t)
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err = worker.Migrate(t.Context(), db.Pool, logger); err != nil {
		t.Fatal(err)
	}
	srv := objecttest.Start(t)
	srv.Bucket(t, "assets-old", true)
	srv.Bucket(t, "assets-new", true)
	old := &player.S3{Client: srv.Client, Bucket: "assets-old"}
	lesson, err := playerfixture.Seed(t.Context(), db.Pool, old, claims.Subject)
	if err != nil {
		t.Fatal(err)
	}
	play := func(objects player.Objects) (int, []any) {
		server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Player: player.New(player.Options{Pool: db.Pool, Objects: objects, AssetOrigin: playerfixture.AssetOrigin})}, logger))
		defer server.Close()
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/v1/lessons/"+lesson+"/manifest", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = resp.Body.Close() }()
		var body struct {
			Slides []any `json:"slides"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body.Slides
	}
	if status, _ := play(&player.S3{Client: srv.Client, Bucket: "assets-new"}); status == http.StatusOK {
		t.Fatal("fallback is off: the lesson must not play from the old bucket")
	}
	status, slides := play(&player.S3{Client: srv.Client, Bucket: "assets-new", Old: old})
	if status != http.StatusOK || len(slides) == 0 {
		t.Fatalf("fallback on: lesson did not play (%d, %d slides)", status, len(slides))
	}
}
