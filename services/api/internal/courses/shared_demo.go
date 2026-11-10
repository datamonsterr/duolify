package courses

import (
	_ "embed"
	"encoding/json"
	"net/http"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
)

// SharedDemoObjectKey is immutable: content changes require a new version/key.
const SharedDemoObjectKey = "lessons/shared-demo/sky-v1/bundle.json"

//go:embed demo/sky-v1.json
var sharedDemoBytes []byte

// SharedDemo is authored public content, independent of learner and financial state.
type SharedDemo struct {
	Kind    string                          `json:"kind"`
	Version string                          `json:"version"`
	Preview *enginev1.PreviewCourseResponse `json:"preview"`
}

// SharedDemoDocuments supplies byte-identical objects for versioned asset backups.
func SharedDemoDocuments() map[string][]byte {
	return map[string][]byte{SharedDemoObjectKey: append([]byte(nil), sharedDemoBytes...)}
}

// ServeSharedDemoHTTP has no engine, learner, database, pricing or wallet dependency.
// E12-08 still gates launch pricing/catalogue activation; this never activates it.
func ServeSharedDemoHTTP(w http.ResponseWriter, _ *http.Request) {
	var demo SharedDemo
	if json.Unmarshal(sharedDemoBytes, &demo) != nil || demo.Kind != "shared_demo" || demo.Version != "sky-v1" || ValidatePreview(demo.Preview) != nil {
		http.Error(w, "Shared example unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("ETag", `"shared-demo-sky-v1"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(sharedDemoBytes)
}
