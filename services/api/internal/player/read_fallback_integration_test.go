//go:build integration

package player_test

import (
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/objecttest"
	"github.com/datamonsterr/duolify/services/api/internal/player"
)

func TestPlayerReadsTryNewThenOld(t *testing.T) {
	srv := objecttest.Start(t)
	srv.Bucket(t, "assets-new", true)
	srv.Bucket(t, "assets-old", true)
	old := &player.S3{Client: srv.Client, Bucket: "assets-old"}
	for key, body := range map[string]string{"lessons/only-old.json": "old", "lessons/both.json": "old both"} {
		if err := old.Put(t.Context(), key, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	fresh := &player.S3{Client: srv.Client, Bucket: "assets-new"}
	if err := fresh.Put(t.Context(), "lessons/both.json", []byte("new both")); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Get(t.Context(), "lessons/only-old.json"); err == nil {
		t.Fatal("fallback must be off by default")
	}
	on := &player.S3{Client: srv.Client, Bucket: "assets-new", Old: old}
	if got, err := on.Get(t.Context(), "lessons/only-old.json"); err != nil || string(got) != "old" {
		t.Fatalf("old-only read: %q %v", got, err)
	}
	if got, err := on.Get(t.Context(), "lessons/both.json"); err != nil || string(got) != "new both" {
		t.Fatalf("new store must win: %q %v", got, err)
	}
	if ok, err := on.Exists(t.Context(), "lessons/only-old.json"); err != nil || !ok {
		t.Fatalf("exists through fallback: %v %v", ok, err)
	}
	if _, err := on.Get(t.Context(), "lessons/none.json"); err == nil {
		t.Fatal("absent in both stores must fail")
	}
	if err := on.Put(t.Context(), "lessons/fresh.json", []byte("fresh")); err != nil {
		t.Fatal(err)
	}
	if ok, _ := old.Exists(t.Context(), "lessons/fresh.json"); ok {
		t.Fatal("write reached the old store")
	}
}
