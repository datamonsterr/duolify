package accounts_test

import (
	"context"
	"encoding/json"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInputBoundaries(t *testing.T) {
	for _, tt := range []struct {
		email string
		ok    bool
	}{
		{" Learner@Example.TEST ", true}, {"a+b@example.test", true}, {"bad", false}, {"Name <a@example.test>", false}, {"a@example.test\nBcc: x", false}, {strings.Repeat("a", 255) + "@example.test", false},
	} {
		_, err := accounts.NormalizeEmail(tt.email)
		if (err == nil) != tt.ok {
			t.Errorf("email %q: %v", tt.email, err)
		}
	}
	if _, err := accounts.New(nil, "short", new(accounts.MailSink)); err == nil {
		t.Fatal("weak signing secret accepted")
	}
}

func TestJWTValidation(t *testing.T) {
	key := strings.Repeat("test-only-key-", 4)
	auth, err := accounts.New(nil, key, new(accounts.MailSink))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name             string
		method           jwt.SigningMethod
		expires          time.Time
		issuer, audience string
		valid            bool
	}{
		{"valid", jwt.SigningMethodHS256, time.Now().Add(15 * time.Minute), "duolify", "duolify-client", true},
		{"expired", jwt.SigningMethodHS256, time.Now().Add(-time.Minute), "duolify", "duolify-client", false},
		{"algorithm", jwt.SigningMethodHS384, time.Now().Add(time.Minute), "duolify", "duolify-client", false},
		{"issuer", jwt.SigningMethodHS256, time.Now().Add(time.Minute), "other", "duolify-client", false},
		{"audience", jwt.SigningMethodHS256, time.Now().Add(time.Minute), "duolify", "other", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			token := jwt.NewWithClaims(tt.method, jwt.RegisteredClaims{Subject: "56d4e614-7665-4f14-b83e-8b5e9bed384a", ID: "8c940797-a555-421e-8dad-b4df96b02213", Issuer: tt.issuer, Audience: jwt.ClaimStrings{tt.audience}, ExpiresAt: jwt.NewNumericDate(tt.expires)})
			raw, err := token.SignedString([]byte(key))
			if err != nil {
				t.Fatal(err)
			}
			_, err = auth.ParseAccess(raw)
			if (err == nil) != tt.valid {
				t.Fatalf("parse %v", err)
			}
		})
	}
}

func TestResendAdapter(t *testing.T) {
	var delivered bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/emails" || r.Header.Get("Authorization") != "Bearer test-only" {
			t.Error("wrong Resend request")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["from"] != "Duolify <login@example.test>" || !strings.Contains(body["text"].(string), "123456") {
			t.Error("wrong mail payload")
		}
		delivered = true
		w.WriteHeader(200)
	}))
	defer server.Close()
	mail := accounts.ResendMailer{APIKey: "test-only", From: "Duolify <login@example.test>", Client: server.Client(), Endpoint: server.URL + "/emails"}
	if err := mail.SendCode(context.Background(), "learner@example.test", "123456"); err != nil || !delivered {
		t.Fatal("mail not delivered", err)
	}
}
