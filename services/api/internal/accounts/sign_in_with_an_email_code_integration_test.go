//go:build integration

package accounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestSignInRacesRotationRestoreAndDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sink := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-only-key-", 4), sink)
	if err != nil {
		t.Fatal(err)
	}
	call := func(path string, body any, token string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequestWithContext(ctx, "POST", path, bytes.NewReader(raw))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if path == "/v1/me" {
			req.Method = "GET"
		}
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, req)
		return w
	}
	if w := call("/v1/me", nil, ""); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	email := "learner@example.test"
	if w := call("/v1/auth/code", map[string]any{"email": email, "age_confirmed": false}, ""); w.Code != 400 {
		t.Fatalf("age gate %d", w.Code)
	}
	if w := call("/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); w.Code != 202 {
		t.Fatalf("request %d %s", w.Code, w.Body)
	}
	code := sink.Code(email)
	if code == "" {
		t.Fatal("mail sink empty")
	}
	var originalHash string
	if err := db.Pool.QueryRow(ctx, "SELECT code_hash FROM auth_codes WHERE email=$1", email).Scan(&originalHash); err != nil {
		t.Fatal(err)
	}
	if w := call("/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); w.Code != 202 {
		t.Fatal("cooldown should be opaque", w.Code)
	}
	var preservedHash string
	if err := db.Pool.QueryRow(ctx, "SELECT code_hash FROM auth_codes WHERE email=$1", email).Scan(&preservedHash); err != nil || preservedHash != originalHash {
		t.Fatal("resend overwrote code", err)
	}
	var originalExpiry time.Time
	// This scenario performs extra resends to inspect preservation; request-IP
	// enforcement is still checked below, and email/IP isolation has its own test.
	auth.ConfigureRateLimits(accounts.RateLimits{RequestIP: 20, RequestEmail: 100, VerifyIP: 60})
	if err := db.Pool.QueryRow(ctx, "UPDATE auth_codes SET attempts=2,last_sent_at=now()-interval '61 seconds' WHERE email=$1 RETURNING expires_at", email).Scan(&originalExpiry); err != nil {
		t.Fatal(err)
	}
	if w := call("/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); w.Code != 202 {
		t.Fatal("resend after cooldown", w.Code)
	}
	var preservedExpiry time.Time
	var preservedAttempts int
	if err := db.Pool.QueryRow(ctx, "SELECT code_hash,expires_at,attempts FROM auth_codes WHERE email=$1", email).Scan(&preservedHash, &preservedExpiry, &preservedAttempts); err != nil || preservedHash != originalHash || !preservedExpiry.Equal(originalExpiry) || preservedAttempts != 2 || sink.Code(email) != code {
		t.Fatal("resend changed active code, expiry or attempts", err)
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	var session accounts.Tokens
	for range 8 {
		wg.Go(func() {
			w := call("/v1/auth/verify", map[string]any{"email": email, "code": code, "age_confirmed": true, "locale": "vi-VN", "time_zone": "Asia/Ho_Chi_Minh"}, "")
			if w.Code == 200 {
				winners.Add(1)
				var got accounts.Tokens
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Error(err)
				}
				session = got
			}
		})
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("verification winners %d", winners.Load())
	}
	if session.ExpiresIn != 900 {
		t.Fatalf("access duration %d", session.ExpiresIn)
	}
	w := call("/v1/me", nil, session.AccessToken)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "vi-VN") || !strings.Contains(w.Body.String(), "Asia/Ho_Chi_Minh") {
		t.Fatalf("profile %d %s", w.Code, w.Body)
	}
	var profile accounts.Profile
	if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.ID == "" || profile.Background != "" {
		t.Fatal("invalid default profile")
	}
	var count int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM learners").Scan(&count); err != nil || count != 1 {
		t.Fatalf("learners %d %v", count, err)
	}
	var hash string
	if err := db.Pool.QueryRow(ctx, "SELECT code_hash FROM auth_codes WHERE email=$1", email).Scan(&hash); err != nil || strings.Contains(hash, code) || len(hash) != 64 {
		t.Fatalf("unsafe code storage %v", err)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT token_hash FROM auth_refresh_tokens").Scan(&hash); err != nil || hash == session.RefreshToken || len(hash) != 64 {
		t.Fatalf("unsafe refresh storage %v", err)
	}
	// A real custom-format backup restores profile and explicit expiry without raw secrets.
	if w := call("/v1/auth/code", map[string]any{"email": "expired@example.test", "age_confirmed": true}, ""); w.Code != 202 {
		t.Fatal(w.Body)
	}
	expiredCode := sink.Code("expired@example.test")
	if _, err := db.Pool.Exec(ctx, "UPDATE auth_codes SET expires_at=now()-interval '1 second' WHERE email='expired@example.test'"); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{
		{"pg_dump", "-U", "duolify", "-d", "duolify", "-Fc", "-f", "/tmp/auth.dump"},
		{"createdb", "-U", "duolify", "restored"},
		{"pg_restore", "-U", "duolify", "-d", "restored", "--exit-on-error", "/tmp/auth.dump"},
	} {
		status, _, err := container.Exec(ctx, command)
		if err != nil || status != 0 {
			t.Fatalf("backup command %v: %d %v", command, status, err)
		}
	}
	restored, err := store.New(ctx, strings.Replace(url, "/duolify?", "/restored?", 1))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.Pool.QueryRow(ctx, "SELECT count(*) FROM learner_profiles WHERE locale='vi-VN'").Scan(&count); err != nil || count != 1 {
		t.Fatal("profile restore failed", err)
	}
	restoredAuth, err := accounts.New(restored.Pool, strings.Repeat("test-only-key-", 4), sink)
	if err != nil {
		t.Fatal(err)
	}
	restoredReq := httptest.NewRequestWithContext(ctx, "GET", "/v1/me", nil)
	restoredReq.Header.Set("Authorization", "Bearer "+session.AccessToken)
	restoredW := httptest.NewRecorder()
	restoredAuth.ServeHTTP(restoredW, restoredReq)
	if restoredW.Code != 200 {
		t.Fatal("restored session failed")
	}
	expiredRaw, _ := json.Marshal(map[string]any{"email": "expired@example.test", "age_confirmed": true, "code": expiredCode})
	expiredW := httptest.NewRecorder()
	restoredAuth.ServeHTTP(expiredW, httptest.NewRequestWithContext(ctx, "POST", "/v1/auth/verify", bytes.NewReader(expiredRaw)))
	if expiredW.Code != 401 {
		t.Fatal("restored expired code was accepted")
	}
	// Expired refresh/session state is authoritative even with a valid JWT signature.
	if _, err := restored.Pool.Exec(ctx, "UPDATE auth_sessions SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	expiredW = httptest.NewRecorder()
	restoredAuth.ServeHTTP(expiredW, restoredReq)
	if expiredW.Code != 401 {
		t.Fatal("expired restored session was accepted")
	}
	expiredRaw, _ = json.Marshal(map[string]string{"refresh_token": session.RefreshToken})
	expiredW = httptest.NewRecorder()
	restoredAuth.ServeHTTP(expiredW, httptest.NewRequestWithContext(ctx, "POST", "/v1/auth/refresh", bytes.NewReader(expiredRaw)))
	if expiredW.Code != 401 {
		t.Fatal("expired restored refresh was accepted")
	}
	// Rotation tolerates races; replay outside the grace revokes the family.
	w = call("/v1/auth/refresh", map[string]string{"refresh_token": session.RefreshToken}, "")
	if w.Code != 200 {
		t.Fatalf("refresh %d %s", w.Code, w.Body)
	}
	var rotated accounts.Tokens
	if err := json.Unmarshal(w.Body.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.RefreshToken == session.RefreshToken {
		t.Fatal("refresh did not rotate")
	}
	if _, err := db.Pool.Exec(ctx, "UPDATE auth_refresh_tokens SET consumed_at=now()-interval '1 minute' WHERE consumed_at IS NOT NULL"); err != nil {
		t.Fatal(err)
	}
	if w := call("/v1/auth/refresh", map[string]string{"refresh_token": session.RefreshToken}, ""); w.Code != 401 {
		t.Fatal("refresh replay accepted")
	}
	if w := call("/v1/me", nil, rotated.AccessToken); w.Code != 401 {
		t.Fatal("family not revoked")
	}
	// Signing in again reuses the identity and profile rather than creating another learner.
	if w := call("/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); w.Code != 202 {
		t.Fatal(w.Body)
	}
	code = sink.Code(email)
	w = call("/v1/auth/verify", map[string]any{"email": email, "code": code, "age_confirmed": true}, "")
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if w := call("/v1/auth/logout", map[string]string{"refresh_token": session.RefreshToken}, ""); w.Code != 204 {
		t.Fatal("logout failed")
	}
	if w := call("/v1/me", nil, session.AccessToken); w.Code != 401 {
		t.Fatal("logout left access active")
	}
	auth.ConfigureRateLimits(accounts.RateLimits{RequestIP: 20, RequestEmail: 100, VerifyIP: 60})
	// Expiry and verification budget remain enforced after a fresh request.
	if w := call("/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); w.Code != 202 {
		t.Fatal(w.Body)
	}
	code = sink.Code(email)
	for range 5 {
		if w := call("/v1/auth/verify", map[string]any{"email": email, "code": "000000", "age_confirmed": true}, ""); w.Code != 401 {
			t.Fatal("invalid code accepted")
		}
	}
	if w := call("/v1/auth/verify", map[string]any{"email": email, "code": code, "age_confirmed": true}, ""); w.Code != 401 {
		t.Fatal("attempt budget bypassed")
	}
	for range 20 {
		call("/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, "")
	}
	if w := call("/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); w.Code != 429 {
		t.Fatal("request IP rate limit bypassed")
	}
	// Cascade removes auth personal data. Codes link by email and follow identity deletion.
	if _, err := db.Pool.Exec(ctx, "DELETE FROM learners WHERE id=$1::uuid", profile.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"auth_identities", "learner_profiles", "auth_sessions", "auth_refresh_tokens", "auth_codes"} {
		if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Errorf("deletion %s: %d %v", table, count, err)
		}
	}
}
