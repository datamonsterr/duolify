//go:build integration

package accounts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// clerkFixture stands in for Clerk: tokens are opaque handles to identities, so the
// integration test never reaches the network. Signature, issuer and azp checks are
// covered by clerk_test.go.
type clerkFixture map[string]accounts.ClerkIdentity

func (f clerkFixture) Verify(_ context.Context, token string) (accounts.ClerkIdentity, error) {
	identity, ok := f[token]
	if !ok {
		return accounts.ClerkIdentity{}, accounts.ErrClerkVerification
	}
	return identity, nil
}

func TestClerkSSOLinkingReplayAndTakeover(t *testing.T) {
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
	signups := &signupEventRecorder{}
	auth.ConfigureProfile(accounts.ProfileOptions{TextLimit: 2000, Events: analytics.WithConsent(signups, analytics.DatabaseConsent(db.Pool))})
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		auth.ServeHTTP(w, httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(raw)))
		return w
	}
	verifier := strings.Repeat("v", 43)
	startBody := map[string]any{"redirect_uri": "duolify://sso/callback", "code_challenge": accounts.PKCEChallenge(verifier), "age_confirmed": true, "locale": "en", "time_zone": "UTC"}

	// Feature off without Clerk keys: config reports it, routes fail closed.
	if off := call("GET", "/v1/auth/sso/config", nil); !strings.Contains(off.Body.String(), `"enabled":false`) {
		t.Fatalf("config without keys: %s", off.Body)
	}
	if off := call("POST", "/v1/auth/sso/start", startBody); off.Code != 503 {
		t.Fatalf("start without keys: %d", off.Code)
	}

	identities := clerkFixture{
		"new-user":        {UserID: "user_new", Email: "Clerk.New@Example.test", EmailVerified: true, Name: "Clerk New"},
		"unverified":      {UserID: "user_unverified", Email: "unverified@example.test", EmailVerified: false},
		"existing-owner":  {UserID: "user_existing", Email: "existing@example.test", EmailVerified: true},
		"existing-rename": {UserID: "user_existing", Email: "renamed@example.test", EmailVerified: true},
		"takeover":        {UserID: "user_attacker", Email: "existing@example.test", EmailVerified: true},
	}
	auth.ConfigureClerk(identities, "pk_test_fixture")
	if on := call("GET", "/v1/auth/sso/config", nil); !strings.Contains(on.Body.String(), `"publishable_key":"pk_test_fixture"`) {
		t.Fatalf("config with keys: %s", on.Body)
	}
	for name, mutate := range map[string]func(map[string]any){
		"unsafe redirect": func(m map[string]any) { m["redirect_uri"] = "https://evil.test/cb" },
		"no age":          func(m map[string]any) { m["age_confirmed"] = false },
		"bad challenge":   func(m map[string]any) { m["code_challenge"] = "short" },
	} {
		body := map[string]any{}
		for k, v := range startBody {
			body[k] = v
		}
		mutate(body)
		if rejected := call("POST", "/v1/auth/sso/start", body); rejected.Code != 400 {
			t.Fatalf("%s accepted: %d", name, rejected.Code)
		}
	}
	begin := func() string {
		w := call("POST", "/v1/auth/sso/start", startBody)
		var tx accounts.SSOTransaction
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &tx) != nil || len(tx.State) < 32 {
			t.Fatalf("start: %d %s", w.Code, w.Body)
		}
		return tx.State
	}
	// lastCode holds the code the hand-off returned to the signing-in browser, per state.
	lastCode := map[string]string{}
	handoff := func(state, token string) int {
		w := call("POST", "/v1/auth/sso/handoff", map[string]any{"state": state, "session_token": token})
		if w.Code == 200 {
			var out struct{ Code string }
			if json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Code) < 32 {
				t.Fatalf("handoff returned no code: %s", w.Body)
			}
			lastCode[state] = out.Code
		}
		return w.Code
	}
	completeWith := func(state, code, codeVerifier string) *httptest.ResponseRecorder {
		return call("POST", "/v1/auth/sso/complete", map[string]any{"state": state, "code": code, "code_verifier": codeVerifier, "redirect_uri": "duolify://sso/callback"})
	}
	complete := func(state, codeVerifier string) *httptest.ResponseRecorder {
		return completeWith(state, lastCode[state], codeVerifier)
	}
	learnerOf := func(w *httptest.ResponseRecorder) string {
		var tokens accounts.Tokens
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &tokens) != nil {
			t.Fatalf("complete: %d %s", w.Code, w.Body)
		}
		claims, err := auth.ParseAccess(tokens.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		return claims.Subject
	}

	// Completion before hand-off, with a wrong verifier, or after replay fails.
	state := begin()
	if w := completeWith(state, strings.Repeat("c", 43), verifier); w.Code != 401 {
		t.Fatalf("complete before handoff: %d", w.Code)
	}
	if code := handoff(state, "unverified"); code != 401 {
		t.Fatalf("unverified email accepted: %d", code)
	}
	if code := handoff(state, "forged-token"); code != 401 {
		t.Fatalf("forged token accepted: %d", code)
	}
	var bindings int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM auth_provider_identities").Scan(&bindings); err != nil || bindings != 0 {
		t.Fatalf("rejected handoffs persisted a binding: %d %v", bindings, err)
	}
	if code := handoff(state, "new-user"); code != 200 {
		t.Fatalf("new-user handoff: %d", code)
	}
	if code := handoff(state, "new-user"); code != 401 {
		t.Fatalf("handoff replay accepted: %d", code)
	}
	if w := complete(state, strings.Repeat("w", 43)); w.Code != 401 {
		t.Fatalf("wrong verifier accepted: %d", w.Code)
	}
	newLearner := learnerOf(complete(state, verifier))
	signups.mu.Lock()
	if len(signups.events) != 1 || signups.events[0]["id"] != newLearner || signups.events[0]["properties"].(map[string]any)["method"] != "clerk" {
		t.Errorf("expected one Clerk signup event, got %#v", signups.events)
	}
	signups.mu.Unlock()
	if w := complete(state, verifier); w.Code != 401 {
		t.Fatalf("completion replay accepted: %d", w.Code)
	}
	var email, display string
	if err = db.Pool.QueryRow(ctx, "SELECT i.email,l.display_name FROM auth_identities i JOIN learners l ON l.id=i.learner_id WHERE l.id=$1::uuid", newLearner).Scan(&email, &display); err != nil || email != "clerk.new@example.test" || display != "Clerk New" {
		t.Fatalf("new learner: %q %q %v", email, display, err)
	}

	// Complete requires the one-time code: state + verifier alone never suffice.
	noCode := begin()
	if handoff(noCode, "new-user") != 200 {
		t.Fatal("handoff")
	}
	if w := completeWith(noCode, "", verifier); w.Code == 200 {
		t.Fatal("complete without a code succeeded")
	}
	// Wrong codes are rejected without consuming the attempt, the right code still works,
	// and the fifth wrong guess burns the attempt so the code cannot be brute forced.
	if w := completeWith(noCode, strings.Repeat("c", 43), verifier); w.Code != 401 {
		t.Fatalf("wrong code: %d", w.Code)
	}
	if learnerOf(complete(noCode, verifier)) != newLearner {
		t.Fatal("a single wrong code consumed the attempt")
	}
	burned := begin()
	if handoff(burned, "new-user") != 200 {
		t.Fatal("handoff")
	}
	for i := 0; i < 5; i++ {
		if w := completeWith(burned, strings.Repeat("c", 43), verifier); w.Code != 401 {
			t.Fatalf("wrong code %d: %d", i, w.Code)
		}
	}
	if w := complete(burned, verifier); w.Code != 401 {
		t.Fatalf("attempt survived five wrong codes: %d", w.Code)
	}
	// An expired code is rejected.
	expired := begin()
	if handoff(expired, "new-user") != 200 {
		t.Fatal("handoff")
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE auth_oauth_attempts SET code_expires_at=now()-interval '1 second' WHERE code_hash IS NOT NULL AND consumed_at IS NULL"); err != nil {
		t.Fatal(err)
	}
	if w := complete(expired, verifier); w.Code != 401 {
		t.Fatalf("expired code accepted: %d", w.Code)
	}
	var stored int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM auth_oauth_attempts WHERE code_hash IS NOT NULL AND code_hash = ANY($1)", []string{lastCode[noCode], lastCode[burned]}).Scan(&stored); err != nil || stored != 0 {
		t.Fatalf("plaintext code stored: %d %v", stored, err)
	}

	// Cross-device attack: the attacker starts SSO in their own app (state A, verifier VA)
	// and sends the victim the bridge link. The victim signs in on the bridge; the code goes
	// to the victim's browser and device. The attacker's app has state and verifier but no
	// code, so it cannot obtain a session, and a code from another attempt does not help.
	attackerVerifier := strings.Repeat("a", 43)
	attackerStart := call("POST", "/v1/auth/sso/start", map[string]any{"redirect_uri": "duolify://sso/callback", "code_challenge": accounts.PKCEChallenge(attackerVerifier), "age_confirmed": true})
	var attackerTx accounts.SSOTransaction
	if err = json.Unmarshal(attackerStart.Body.Bytes(), &attackerTx); err != nil || attackerTx.State == "" {
		t.Fatalf("attacker start: %d", attackerStart.Code)
	}
	if handoff(attackerTx.State, "existing-owner") != 200 {
		t.Fatal("victim handoff")
	}
	victimCode := lastCode[attackerTx.State]
	if w := completeWith(attackerTx.State, "", attackerVerifier); w.Code == 200 {
		t.Fatal("attacker completed with state and verifier only")
	}
	if w := completeWith(attackerTx.State, lastCode[state], attackerVerifier); w.Code != 401 {
		t.Fatalf("attacker used another attempt's code: %d", w.Code)
	}
	// Only the holder of both the code and the verifier gets a session (the victim's own app).
	if w := completeWith(attackerTx.State, victimCode, verifier); w.Code != 401 {
		t.Fatalf("code with the wrong verifier accepted: %d", w.Code)
	}

	// Signing in again with the same Clerk user returns the same learner.
	again := begin()
	if handoff(again, "new-user") != 200 || learnerOf(complete(again, verifier)) != newLearner {
		t.Fatal("returning Clerk user did not resolve to the same learner")
	}
	signups.mu.Lock()
	var newLearnerEvents int
	for _, event := range signups.events {
		if event["id"] == newLearner {
			newLearnerEvents++
		}
	}
	if newLearnerEvents != 1 {
		t.Errorf("returning sign-in emitted signup: %#v", signups.events)
	}
	signups.mu.Unlock()

	// An email-code account is linked by Clerk-verified email, then keyed by Clerk id.
	if r := call("POST", "/v1/auth/code", map[string]any{"email": "existing@example.test", "age_confirmed": true}); r.Code != 202 {
		t.Fatalf("code request: %d", r.Code)
	}
	code := sink.Code("existing@example.test")
	existingLearner := learnerOf(call("POST", "/v1/auth/verify", map[string]any{"email": "existing@example.test", "code": code, "age_confirmed": true}))
	linked := begin()
	if handoff(linked, "existing-owner") != 200 || learnerOf(complete(linked, verifier)) != existingLearner {
		t.Fatal("verified email did not link to the existing learner")
	}
	renamed := begin()
	if handoff(renamed, "existing-rename") != 200 || learnerOf(complete(renamed, verifier)) != existingLearner {
		t.Fatal("bound Clerk user changed learner after an email change")
	}

	// A second Clerk user with the same verified email must not take the account over.
	hostile := begin()
	if code := handoff(hostile, "takeover"); code != 409 {
		t.Fatalf("takeover by second Clerk user: %d", code)
	}
	if w := completeWith(hostile, strings.Repeat("c", 43), verifier); w.Code != 401 {
		t.Fatalf("takeover attempt issued tokens: %d", w.Code)
	}
	var users int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM auth_provider_identities WHERE learner_id=$1::uuid", existingLearner).Scan(&users); err != nil || users != 1 {
		t.Fatalf("learner has %d Clerk bindings: %v", users, err)
	}

	// Expired attempts cannot be handed off.
	late := begin()
	if _, err = db.Pool.Exec(ctx, "UPDATE auth_oauth_attempts SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if handoff(late, "new-user") != 401 {
		t.Fatal("expired attempt accepted")
	}
}
