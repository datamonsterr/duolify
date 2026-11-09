package accounts

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// NewTestService creates a lightweight Service without Postgres for unit testing.
func NewTestService(t *testing.T) *Service {
	t.Helper()
	s, err := New(nil, strings.Repeat("test-key-", 5), new(MailSink))
	if err != nil {
		t.Fatal(err)
	}
	s.ConfigureProfile(ProfileOptions{TextLimit: 2000})
	return s
}

// IssueTestToken creates an authenticated access token for a test learner.
func (s *Service) IssueTestToken(learnerID string, _ bool) string {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "duolify",
		Audience:  jwt.ClaimStrings{"duolify-client"},
		Subject:   learnerID,
		ID:        "8c940797-a555-421e-8dad-b4df96b02213",
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
	})
	signed, _ := token.SignedString(s.key)
	return signed
}

// WithTestLearner adds authenticated learner identity to request context.
func WithTestLearner(ctx context.Context, learnerID string) context.Context {
	return context.WithValue(ctx, learnerKey{}, learnerID)
}

// HandleSetupChat exports internal handler for unit testing.
func (s *Service) HandleSetupChat(w http.ResponseWriter, r *http.Request) {
	s.handleSetupChat(w, r)
}
