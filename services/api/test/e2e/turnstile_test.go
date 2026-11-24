package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/accountstest"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
	"github.com/google/uuid"
)

func TestTurnstileProtectedHTTPFlows(t *testing.T) {
	ownerAuth, access, db := signedInDatabase(t)
	owner, err := ownerAuth.ParseAccess(access)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	var courseID string
	if err := db.Pool.QueryRow(ctx, `WITH quote AS (
INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at)
SELECT $1::uuid,'turnstile-preview-fixture',version,'{}',now(),now()+interval '1 hour' FROM pricing_quote_policies ORDER BY version LIMIT 1 RETURNING id
), creation AS (
INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state)
SELECT $1::uuid,gen_random_uuid(),1,id,'{}','enrolled' FROM quote RETURNING id
)
INSERT INTO courses(learner_id,creation_id,title,visibility)
SELECT $1::uuid,id,'Turnstile preview fixture','link' FROM creation RETURNING id::text`, owner.Subject).Scan(&courseID); err != nil {
		t.Fatal(err)
	}
	shareToken := uuid.NewString()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO publication_drafts(course_id,owner_id,number,content_hash,snapshot,lesson_count,verdict,sampled_lessons)
VALUES($1,$2,1,repeat('b',64),'{"title":"Turnstile preview fixture","units":[]}',1,'pass',0)`, courseID, owner.Subject); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO publication_links(course_id,token_hash) VALUES($1,$2)`, courseID, sharing.HashToken(shareToken)); err != nil {
		t.Fatal(err)
	}
	sink := new(accounts.MailSink)
	newRouter := func(secret string) http.Handler {
		t.Helper()
		auth, err := accounts.New(db.Pool, "test-only-signing-key-at-least-32-bytes", sink)
		if err != nil {
			t.Fatal(err)
		}
		gate := accounts.NewTurnstile(accountstest.TurnstileSiteKey, accounts.NewSiteverify(secret, "", []string{accountstest.TurnstileHostname}, accountstest.TurnstileClient()), nil)
		auth.ConfigureTurnstile(gate)
		s := sharing.NewService(db.Pool, nil, sharing.DefaultConfig()).WithTurnstile(gate)
		return httpapi.New(httpapi.Dependencies{Accounts: auth, Sharing: s}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	pass := newRouter(accountstest.TurnstilePassSecret)
	fail := newRouter(accountstest.TurnstileFailSecret)
	call := func(router http.Handler, method, path string, body any, proof string, status int) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(raw))
		if proof != "" {
			r.Header.Set("X-Turnstile-Token", proof)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != status || status == 400 && !strings.Contains(w.Body.String(), `"code":"turnstile_failed"`) {
			t.Fatalf("%s %s: status %d want %d; %s", method, path, w.Code, status, w.Body)
		}
		return w
	}
	const email = "turnstile-e2e@example.test"
	data := map[string]any{"email": email, "age_confirmed": true, "turnstile_token": "email_code:" + uuid.NewString()}
	call(fail, http.MethodPost, "/v1/auth/code", data, "", http.StatusBadRequest)
	if sink.Code(email) != "" {
		t.Fatal("failed proof sent an email")
	}
	data["turnstile_token"] = "email_code:fresh"
	call(pass, http.MethodPost, "/v1/auth/code", data, "", http.StatusAccepted)
	data["code"] = sink.Code(email)
	data["turnstile_token"] = "sign_up:rejected"
	call(fail, http.MethodPost, "/v1/auth/verify", data, "", http.StatusBadRequest)
	data["turnstile_token"] = "sign_up:fresh"
	call(pass, http.MethodPost, "/v1/auth/verify", data, "", http.StatusOK)

	path := "/v1/public/shares/" + shareToken
	w := call(fail, http.MethodGet, path, nil, "share_preview:rejected", http.StatusBadRequest)
	if strings.Contains(w.Body.String(), "Turnstile preview fixture") || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatal("challenge refusal exposed preview or lost safety headers")
	}
	w = call(pass, http.MethodGet, path, nil, "share_preview:fresh", http.StatusOK)
	if !strings.Contains(w.Body.String(), "Turnstile preview fixture") {
		t.Fatal("verified share did not return preview")
	}
}
