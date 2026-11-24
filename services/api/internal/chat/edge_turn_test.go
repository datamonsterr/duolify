package chat

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEdgeServiceTokenBindsOperation(t *testing.T) {
	const path = "/internal/edge/chat/messages"
	secret := strings.Repeat("test-key", 4)
	now := time.Unix(1780000000, 0)
	body := []byte(`{"idempotency_key":"turn-a"}`)
	claims := edgeServiceClaims{Exp: now.Add(30 * time.Second).Unix(), Method: http.MethodPost, Path: path, BodySHA256: bodyDigest(body), Transport: "websocket"}
	token, err := signEdge(secret, edgeServiceDomain, claims)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, method, path, body string
		now                      time.Time
		ok                       bool
	}{
		{"valid", http.MethodPost, path, string(body), now, true},
		{"expired", http.MethodPost, path, string(body), now.Add(30 * time.Second), false},
		{"excessive lifetime", http.MethodPost, path, string(body), now.Add(-time.Minute), false},
		{"wrong method", http.MethodGet, path, string(body), now, false},
		{"wrong path", http.MethodPost, path + "/other", string(body), now, false},
		{"query", http.MethodPost, path + "?extra=1", string(body), now, false},
		{"wrong body", http.MethodPost, path, `{"idempotency_key":"turn-b"}`, now, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil)
			r.Header.Set("Authorization", "Bearer "+token)
			_, err := verifyEdgeService(secret, r, []byte(tt.body), tt.now)
			if (err == nil) != tt.ok {
				t.Fatalf("verified = %v, want %v", err == nil, tt.ok)
			}
		})
	}
	var scope edgeScope
	if err := verifyEdge(secret, edgeChannelDomain, token, &scope); err == nil {
		t.Fatal("service token was accepted as a learner channel grant")
	}
}
