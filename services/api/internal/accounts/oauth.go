package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrOAuthProvider marks a rejected or unverifiable provider response.
var ErrOAuthProvider = errors.New("OAuth provider rejected authorization")
var errOAuthTransaction = errors.New("invalid or expired OAuth transaction")
var errOAuthLinking = errors.New("account linking required")

// OAuthAuthorizationRequest contains only public protocol values.
type OAuthAuthorizationRequest struct {
	State, Nonce, CodeChallenge, RedirectURI string
}

// OAuthExchangeRequest is passed to a provider adapter after state/PKCE validation.
type OAuthExchangeRequest struct {
	Code, CodeVerifier, RedirectURI, Nonce string
}

// ProviderIdentity contains claims the adapter has verified against provider keys,
// issuer and client audience. The API additionally verifies the transaction nonce.
type ProviderIdentity struct {
	Provider, Subject, Email, Nonce string
	EmailVerified                   bool
}

// OAuthProvider is provider-neutral so Apple can be added without changing sessions.
type OAuthProvider interface {
	AuthorizationURL(context.Context, OAuthAuthorizationRequest) (string, error)
	Exchange(context.Context, OAuthExchangeRequest) (ProviderIdentity, error)
}

// OAuthTransaction is returned once to the initiating client and is never persisted
// in plaintext except for values that are not credentials (nonce and redirect URI).
type OAuthTransaction struct {
	State            string `json:"state"`
	Nonce            string `json:"nonce"`
	AuthorizationURL string `json:"authorization_url"`
}

type oauthStartInput struct {
	RedirectURI   string `json:"redirect_uri"`
	CodeChallenge string `json:"code_challenge"`
}

type oauthCompleteInput struct {
	State           string `json:"state"`
	Nonce           string `json:"nonce"`
	Code            string `json:"code"`
	CodeVerifier    string `json:"code_verifier"`
	RedirectURI     string `json:"redirect_uri"`
	AgeConfirmed    bool   `json:"age_confirmed"`
	Locale          string `json:"locale"`
	TimeZone        string `json:"time_zone"`
	AnalyticsOptOut bool   `json:"analytics_opt_out"`
}

const oauthRedirectURI = "duolify://oauth/callback"

// ValidOAuthRedirect accepts only the registered OAuth return URI.
func ValidOAuthRedirect(value string) bool { return value == oauthRedirectURI }

// ValidPKCEChallenge validates the unpadded base64url form of a SHA-256 challenge.
func ValidPKCEChallenge(value string) bool {
	if len(value) != 43 {
		return false
	}
	for _, char := range value {
		if char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

// PKCEChallenge returns the RFC 7636 S256 challenge for a verifier.
func PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ValidateProviderIdentity rejects unverified or transaction-mismatched provider claims.
func ValidateProviderIdentity(identity ProviderIdentity, expectedNonce string) error {
	if identity.Provider != "google" && identity.Provider != "apple" {
		return ErrOAuthProvider
	}
	if identity.Subject == "" || len(identity.Subject) > 255 || identity.Email == "" || len(identity.Email) > 254 || !identity.EmailVerified || expectedNonce == "" {
		return ErrOAuthProvider
	}
	if subtle.ConstantTimeCompare([]byte(identity.Nonce), []byte(expectedNonce)) != 1 {
		return ErrOAuthProvider
	}
	if _, err := NormalizeEmail(identity.Email); err != nil {
		return ErrOAuthProvider
	}
	return nil
}

// FixtureGoogleProvider is deterministic local/test behavior. It is intentionally
// tied to .test hosts and must be replaced only when E1-10 provisions live OAuth.
type FixtureGoogleProvider struct{}

// AuthorizationURL creates a deterministic .test authorization URL for local fixtures.
func (p *FixtureGoogleProvider) AuthorizationURL(_ context.Context, request OAuthAuthorizationRequest) (string, error) {
	if !ValidOAuthRedirect(request.RedirectURI) || request.State == "" || request.Nonce == "" || len(request.CodeChallenge) != 43 {
		return "", ErrOAuthProvider
	}
	values := url.Values{"state": {request.State}, "nonce": {request.Nonce}, "code_challenge": {request.CodeChallenge}, "redirect_uri": {request.RedirectURI}}
	return "https://accounts.google.test/authorize?" + values.Encode(), nil
}

// Exchange recognizes only the fixture authorization code and returns synthetic claims.
func (p *FixtureGoogleProvider) Exchange(_ context.Context, request OAuthExchangeRequest) (ProviderIdentity, error) {
	if request.Code != "fixture-google-code" || request.CodeVerifier == "" || !ValidOAuthRedirect(request.RedirectURI) {
		return ProviderIdentity{}, ErrOAuthProvider
	}
	return ProviderIdentity{Provider: "google", Subject: "fixture-google-subject", Email: "learner@example.test", EmailVerified: true, Nonce: request.Nonce}, nil
}

func randomOAuthValue() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

func validateOAuthVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 || strings.ContainsAny(value, "=+/") {
		return false
	}
	for _, char := range value {
		if char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func (s *Service) googleOAuthStart(w http.ResponseWriter, r *http.Request) {
	var in oauthStartInput
	if !decode(w, r, &in) {
		return
	}
	if s.oauth == nil || !ValidOAuthRedirect(in.RedirectURI) || !ValidPKCEChallenge(in.CodeChallenge) {
		respond(w, http.StatusBadRequest, map[string]string{"error": "Invalid sign-in request."})
		return
	}
	if _, err := s.pool.Exec(r.Context(), "DELETE FROM auth_oauth_attempts WHERE expires_at<=now() OR consumed_at IS NOT NULL"); err != nil {
		failure(w, err)
		return
	}
	state, err := randomOAuthValue()
	if err != nil {
		failure(w, err)
		return
	}
	nonce, err := randomOAuthValue()
	if err != nil {
		failure(w, err)
		return
	}
	transaction := OAuthAuthorizationRequest{State: state, Nonce: nonce, CodeChallenge: in.CodeChallenge, RedirectURI: in.RedirectURI}
	urlValue, err := s.oauth.AuthorizationURL(r.Context(), transaction)
	if err != nil {
		failure(w, err)
		return
	}
	_, err = s.pool.Exec(r.Context(), `INSERT INTO auth_oauth_attempts(state_hash,nonce_hash,code_challenge,redirect_uri,expires_at) VALUES($1,$2,$3,$4,now()+interval '10 minutes')`, s.hmac(state), s.hmac(nonce), in.CodeChallenge, in.RedirectURI)
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, http.StatusOK, OAuthTransaction{State: state, Nonce: nonce, AuthorizationURL: urlValue})
}

func (s *Service) googleOAuthComplete(w http.ResponseWriter, r *http.Request) {
	var in oauthCompleteInput
	if !decode(w, r, &in) {
		return
	}
	if s.oauth == nil || !ValidOAuthRedirect(in.RedirectURI) || !in.AgeConfirmed || !validateOAuthVerifier(in.CodeVerifier) || len(in.State) < 32 || len(in.Nonce) < 32 || len(in.Code) == 0 || len(in.Code) > 2048 {
		respond(w, http.StatusBadRequest, map[string]string{"error": "Invalid sign-in request."})
		return
	}
	if len(in.Locale) == 0 {
		in.Locale = "en"
	}
	if len(in.TimeZone) == 0 {
		in.TimeZone = "UTC"
	}
	if len(in.Locale) > 64 || !localePattern.MatchString(in.Locale) || len(in.TimeZone) > 64 {
		respond(w, http.StatusBadRequest, map[string]string{"error": "Invalid sign-in request."})
		return
	}
	if _, err := time.LoadLocation(in.TimeZone); err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "Invalid sign-in request."})
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		failure(w, err)
		return
	}
	var nonceHash, challenge, redirect string
	var active bool
	err = tx.QueryRow(ctx, `SELECT nonce_hash,code_challenge,redirect_uri,expires_at>now() AND consumed_at IS NULL FROM auth_oauth_attempts WHERE state_hash=$1 FOR UPDATE`, s.hmac(in.State)).Scan(&nonceHash, &challenge, &redirect, &active)
	if err != nil || !active || redirect != in.RedirectURI || subtle.ConstantTimeCompare([]byte(challenge), []byte(PKCEChallenge(in.CodeVerifier))) != 1 || subtle.ConstantTimeCompare([]byte(nonceHash), []byte(s.hmac(in.Nonce))) != 1 {
		_ = tx.Rollback(ctx)
		respond(w, http.StatusUnauthorized, map[string]string{"error": "Sign-in could not be verified. Please retry."})
		return
	}
	if _, err = tx.Exec(ctx, "UPDATE auth_oauth_attempts SET consumed_at=now() WHERE state_hash=$1", s.hmac(in.State)); err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(w, err)
		return
	}
	identity, err := s.oauth.Exchange(ctx, OAuthExchangeRequest{Code: in.Code, CodeVerifier: in.CodeVerifier, RedirectURI: in.RedirectURI, Nonce: in.Nonce})
	if err != nil || identity.Provider != "google" || ValidateProviderIdentity(identity, identity.Nonce) != nil || subtle.ConstantTimeCompare([]byte(nonceHash), []byte(s.hmac(identity.Nonce))) != 1 {
		respond(w, http.StatusUnauthorized, map[string]string{"error": "Sign-in could not be verified. Please retry."})
		return
	}
	tokens, err := s.bindProviderAndIssue(ctx, identity, in)
	if errors.Is(err, errOAuthLinking) {
		respond(w, http.StatusConflict, map[string]string{"error": "This account needs to be linked before sign-in. Use another sign-in method."})
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, http.StatusOK, tokens)
}

func (s *Service) bindProviderAndIssue(ctx context.Context, identity ProviderIdentity, in oauthCompleteInput) (Tokens, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Tokens{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var learnerID string
	err = tx.QueryRow(ctx, "SELECT learner_id::text FROM auth_provider_identities WHERE provider=$1 AND subject=$2", identity.Provider, identity.Subject).Scan(&learnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		email, emailErr := NormalizeEmail(identity.Email)
		if emailErr != nil {
			return Tokens{}, ErrOAuthProvider
		}
		err = tx.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&learnerID)
		if err == nil {
			return Tokens{}, errOAuthLinking
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Tokens{}, err
		}
		err = tx.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('Learner') RETURNING id::text").Scan(&learnerID)
		if err != nil {
			return Tokens{}, err
		}
		_, err = tx.Exec(ctx, "INSERT INTO auth_identities(email,learner_id) VALUES($1,$2::uuid)", email, learnerID)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO learner_profiles(learner_id,locale,time_zone,analytics_opt_out) VALUES($1::uuid,$2,$3,$4)`, learnerID, in.Locale, in.TimeZone, in.AnalyticsOptOut)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO auth_provider_identities(provider,subject,learner_id,verified_email) VALUES($1,$2,$3::uuid,$4)`, identity.Provider, identity.Subject, learnerID, email)
		}
		if err != nil {
			return Tokens{}, err
		}
	} else if err != nil {
		return Tokens{}, err
	}
	var sessionID string
	var expiry time.Time
	if err = tx.QueryRow(ctx, "INSERT INTO auth_sessions(learner_id) VALUES($1::uuid) RETURNING id::text,expires_at", learnerID).Scan(&sessionID, &expiry); err != nil {
		return Tokens{}, err
	}
	tokens, err := s.issue(ctx, tx, learnerID, sessionID, expiry)
	if err != nil {
		return Tokens{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Tokens{}, err
	}
	return tokens, nil
}

var _ OAuthProvider = (*FixtureGoogleProvider)(nil)
