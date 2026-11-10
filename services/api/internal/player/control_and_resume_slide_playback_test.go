package player

import (
	"strings"
	"testing"
)

func TestPlaybackPositionValidation(t *testing.T) {
	m := ObjectManifest{Version: 1, Slides: []ObjectSlide{{ID: "sky-1", DurationMS: 3000}}}
	for _, body := range []string{
		`{"version":1,"slide_id":"sky-1","time_ms":0}`,
		`{"version":1,"slide_id":"sky-1","time_ms":3000}`,
	} {
		if _, err := decodePosition(strings.NewReader(body), m); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{
		`{}`, `null`, `[]`,
		`{"version":1,"slide_id":"sky-1"}`,
		`{"version":1,"slide_id":"sky-1","time_ms":null}`,
		`{"version":1,"slide_id":"sky-1","time_ms":-1}`,
		`{"version":1,"slide_id":"sky-1","time_ms":3001}`,
		`{"version":2,"slide_id":"sky-1","time_ms":0}`,
		`{"version":1,"slide_id":"foreign","time_ms":0}`,
		`{"version":1,"slide_id":"sky-1","time_ms":0.5}`,
		`{"version":1,"slide_id":"sky-1","time_ms":0,"completed":true}`,
		`{"version":1,"slide_id":"sky-1","time_ms":0} {}`,
	} {
		if _, err := decodePosition(strings.NewReader(body), m); err == nil {
			t.Fatal("accepted", body)
		}
	}
}
