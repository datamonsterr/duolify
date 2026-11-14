package player_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/player"
)

type generatedWriter struct {
	docs map[string][]byte
	fail bool
}

func (w *generatedWriter) Put(_ context.Context, key string, raw []byte) error {
	if w.fail {
		return errors.New("storage unavailable")
	}
	if old, ok := w.docs[key]; ok && string(old) != string(raw) {
		return errors.New("immutable key collision")
	}
	w.docs[key] = raw
	return nil
}

func TestPublishGeneratedImmutableAndValidated(t *testing.T) {
	w := &generatedWriter{docs: map[string][]byte{}}
	slides := []player.GeneratedSlide{{ID: "s1", HTML: `<div id="root">Sky</div>`, DurationMS: 1000, Narration: "The sky."}}
	key, err := player.PublishGenerated(t.Context(), w, "lessons/generated/job", slides, "https://assets.example.test")
	if err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for k, v := range w.docs {
		before[k] = v
	}
	again, err := player.PublishGenerated(t.Context(), w, "lessons/generated/job", slides, "https://assets.example.test")
	if err != nil || key != again || !reflect.DeepEqual(before, w.docs) {
		t.Fatal("retry changed immutable objects", err)
	}
	slides[0].Narration = "Changed narration."
	changed, err := player.PublishGenerated(t.Context(), w, "lessons/generated/job", slides, "https://assets.example.test")
	if err != nil || changed == key {
		t.Fatal("changed content reused immutable key", err)
	}
	count := len(w.docs)
	slides[0].HTML = `<div id="root"><iframe src="https://evil.test"></iframe></div>`
	if _, err = player.PublishGenerated(t.Context(), w, "lessons/generated/job", slides, "https://assets.example.test"); err == nil || len(w.docs) != count {
		t.Fatal("invalid content reached storage")
	}
	slides[0].HTML = `<div id="root">Safe</div>`
	w.fail = true
	if _, err = player.PublishGenerated(t.Context(), w, "lessons/generated/job", slides, "https://assets.example.test"); err == nil {
		t.Fatal("publication error hidden")
	}
	if _, err = player.PublishGenerated(t.Context(), nil, "lessons/generated/job", slides, "https://assets.example.test"); err == nil {
		t.Fatal("missing writer accepted")
	}
}
