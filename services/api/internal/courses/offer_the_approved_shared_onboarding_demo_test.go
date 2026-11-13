package courses

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestSharedDemoAuthoredVersionAndReadOnly(t *testing.T) {
	first := httptest.NewRecorder()
	ServeSharedDemoHTTP(first, httptest.NewRequestWithContext(t.Context(), "GET", "/v1/courses/shared-demo?topic=private-input", nil))
	if first.Code != 200 {
		t.Fatal(first.Code)
	}
	var demo SharedDemo
	if err := json.Unmarshal(first.Body.Bytes(), &demo); err != nil {
		t.Fatal(err)
	}
	if demo.Kind != "shared_demo" || demo.Version != "sky-v1" || ValidatePreview(demo.Preview) != nil {
		t.Fatal("invalid authored content", demo)
	}
	if bytes.Contains(first.Body.Bytes(), []byte("private-input")) {
		t.Fatal("personalized the demo")
	}
	second := httptest.NewRecorder()
	ServeSharedDemoHTTP(second, httptest.NewRequestWithContext(t.Context(), "GET", "/v1/courses/shared-demo", nil))
	if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatal("demo changed by request")
	}
	if !bytes.Equal(SharedDemoDocuments()[SharedDemoObjectKey], first.Body.Bytes()) {
		t.Fatal("object backup differs from served bundle")
	}
	// Callers cannot mutate future responses or backup bytes.
	doc := SharedDemoDocuments()
	doc[SharedDemoObjectKey][0] = 'x'
	if SharedDemoDocuments()[SharedDemoObjectKey][0] == 'x' {
		t.Fatal("mutable shared bytes")
	}
}
