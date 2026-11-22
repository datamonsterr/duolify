package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Clerk SSO (ADR 0016). RFC 8252 shape: PKCE verifier (app) + one-time code (browser that signed in). Clerk authenticates the learner in a browser on a normal web
// origin; the API verifies the Clerk session token itself, links or creates the
// Duolify account, and issues the existing Duolify session. The native app never
// sees a Clerk token. The hand-off returns a one-time code to the browser that signed
// in; the app on that device receives it through the deep link and redeems it with its
// PKCE verifier. A different device's app never sees the code, and an app that only
// intercepts the deep link lacks the verifier.

var errSSOLinking = errors.New("account is already linked to another Clerk user")
var errSSOState = errors.New("invalid or expired SSO transaction")

const ssoRedirectURI = "duolify://sso/callback"

// ValidSSORedirect accepts only the registered deep-link return URI.
func ValidSSORedirect(value string) bool { return value == ssoRedirectURI }

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

func randomState() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

func validPKCEVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
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

// ConfigureClerk enables SSO. Without both a verifier and a publishable key every
// SSO route reports the feature as off and email-code sign-in is unaffected.
func (s *Service) ConfigureClerk(verifier ClerkVerifier, publishableKey string) {
	s.clerk = verifier
	s.clerkKey = publishableKey
}

func (s *Service) ssoEnabled() bool { return s.clerk != nil && s.clerkKey != "" }

func (s *Service) ssoConfig(w http.ResponseWriter, _ *http.Request) {
	if !s.ssoEnabled() {
		respond(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	respond(w, http.StatusOK, map[string]any{"enabled": true, "publishable_key": s.clerkKey})
}

func ssoUnavailable(w http.ResponseWriter) {
	respond(w, http.StatusServiceUnavailable, map[string]string{"error": "Single sign-on is not available. Use an email code."})
}

type ssoStartInput struct {
	RedirectURI     string `json:"redirect_uri"`
	CodeChallenge   string `json:"code_challenge"`
	AgeConfirmed    bool   `json:"age_confirmed"`
	Locale          string `json:"locale"`
	TimeZone        string `json:"time_zone"`
	AnalyticsOptOut bool   `json:"analytics_opt_out"`
}

// SSOTransaction is returned once to the initiating client.
type SSOTransaction struct {
	State string `json:"state"`
}

func (s *Service) ssoStart(w http.ResponseWriter, r *http.Request) {
	if !s.ssoEnabled() {
		ssoUnavailable(w)
		return
	}
	var in ssoStartInput
	if !decode(w, r, &in) {
		return
	}
	if in.Locale == "" {
		in.Locale = "en"
	}
	if in.TimeZone == "" {
		in.TimeZone = "UTC"
	}
	if _, err := time.LoadLocation(in.TimeZone); err != nil || !in.AgeConfirmed || !ValidSSORedirect(in.RedirectURI) || !ValidPKCEChallenge(in.CodeChallenge) || len(in.Locale) > 64 || !localePattern.MatchString(in.Locale) || len(in.TimeZone) > 64 {
		respond(w, http.StatusBadRequest, map[string]string{"error": "Invalid sign-in request."})
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		failure(w, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = s.limit(ctx, tx, "sso-start-ip:"+s.clientIP(r), s.limits.VerifyIP); err != nil {
		if errors.Is(err, errLimited) {
			err = errors.Join(err, tx.Commit(ctx))
		}
		failure(w, err)
		return
	}
	state, err := randomState()
	if err != nil {
		failure(w, err)
		return
	}
	if _, err = tx.Exec(ctx, "DELETE FROM auth_oauth_attempts WHERE expires_at<=now() OR consumed_at IS NOT NULL"); err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO auth_oauth_attempts(state_hash,code_challenge,redirect_uri,locale,time_zone,analytics_opt_out,expires_at) VALUES($1,$2,$3,$4,$5,$6,now()+interval '10 minutes')`,
			keyedHash(s.oauthKey, state), in.CodeChallenge, in.RedirectURI, in.Locale, in.TimeZone, in.AnalyticsOptOut)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, http.StatusOK, SSOTransaction{State: state})
}

func decodeSized(w http.ResponseWriter, r *http.Request, value any, limit int64) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "Invalid request."})
		return false
	}
	return true
}

type ssoHandoffInput struct {
	State        string `json:"state"`
	SessionToken string `json:"session_token"`
}

// ssoHandoff is called by the web bridge page after Clerk signs the learner in. It
// verifies the Clerk token and binds the resulting Duolify learner to the attempt.
func (s *Service) ssoHandoff(w http.ResponseWriter, r *http.Request) {
	if !s.ssoEnabled() {
		ssoUnavailable(w)
		return
	}
	var in ssoHandoffInput
	if !decodeSized(w, r, &in, 16384) {
		return
	}
	if len(in.State) < 32 || len(in.State) > 128 || in.SessionToken == "" {
		respond(w, http.StatusBadRequest, map[string]string{"error": "Invalid sign-in request."})
		return
	}
	ctx := r.Context()
	if err := s.ssoThrottle(ctx, "sso-handoff-ip:"+s.clientIP(r), s.limits.VerifyIP); err != nil {
		failure(w, err)
		return
	}
	// Verify before any row lock: the Clerk Backend API call can be slow.
	identity, err := s.clerk.Verify(ctx, in.SessionToken)
	if errors.Is(err, ErrClerkVerification) {
		respond(w, http.StatusUnauthorized, map[string]string{"error": "Sign-in could not be verified. Please retry."})
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	code, err := s.bindClerkIdentity(ctx, identity, keyedHash(s.oauthKey, in.State))
	switch {
	case errors.Is(err, errSSOState), errors.Is(err, ErrClerkVerification):
		respond(w, http.StatusUnauthorized, map[string]string{"error": "Sign-in could not be verified. Please retry."})
	case errors.Is(err, errSSOLinking):
		respond(w, http.StatusConflict, map[string]string{"error": "This email is already linked to a different sign-in. Use an email code."})
	case err != nil:
		failure(w, err)
	default:
		// The code goes only to the browser that completed Clerk sign-in. It is the
		// proof that the device holding the attempt is the device that signed in.
		respond(w, http.StatusOK, map[string]string{"code": code})
	}
}

func (s *Service) ssoThrottle(ctx context.Context, key string, maximum int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = s.limit(ctx, tx, key, maximum)
	if err == nil || errors.Is(err, errLimited) {
		if commit := tx.Commit(ctx); commit != nil {
			return commit
		}
	}
	return err
}

// bindClerkIdentity links or creates the learner and records it on the attempt.
//
// Linking rules (ADR 0016): a known Clerk user id always wins and its email is
// ignored; otherwise the Clerk primary email must be Clerk-verified. A learner that
// already has a different Clerk user bound is never re-bound by email.
func (s *Service) bindClerkIdentity(ctx context.Context, identity ClerkIdentity, stateHash string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locale, zone string
	var optOut, active bool
	err = tx.QueryRow(ctx, `SELECT locale,time_zone,analytics_opt_out,expires_at>now() AND consumed_at IS NULL AND handed_off_at IS NULL FROM auth_oauth_attempts WHERE state_hash=$1 FOR UPDATE`, stateHash).Scan(&locale, &zone, &optOut, &active)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !active {
		return "", errSSOState
	}
	if err != nil {
		return "", err
	}
	var learnerID string
	err = tx.QueryRow(ctx, "SELECT learner_id::text FROM auth_provider_identities WHERE provider='clerk' AND subject=$1", identity.UserID).Scan(&learnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		learnerID, err = s.linkOrCreate(ctx, tx, identity, locale, zone, optOut)
	}
	if err != nil {
		return "", err
	}
	if optOut {
		if _, err = tx.Exec(ctx, "UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id=$1::uuid", learnerID); err != nil {
			return "", err
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE auth_oauth_attempts SET learner_id=$2::uuid,clerk_user_id=$3,handed_off_at=now() WHERE state_hash=$1", stateHash, learnerID, identity.UserID); err != nil {
		return "", err
	}
	code, err := randomState()
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, "UPDATE auth_oauth_attempts SET code_hash=$2,code_expires_at=now()+interval '2 minutes',code_attempts=0 WHERE state_hash=$1", stateHash, ssoCodeHash(s.oauthKey, code)); err != nil {
		return "", err
	}
	return code, tx.Commit(ctx)
}

func (s *Service) linkOrCreate(ctx context.Context, tx pgx.Tx, identity ClerkIdentity, locale, zone string, optOut bool) (string, error) {
	if identity.Email == "" || !identity.EmailVerified {
		return "", ErrClerkVerification
	}
	email, err := NormalizeEmail(identity.Email)
	if err != nil {
		return "", ErrClerkVerification
	}
	var learnerID string
	err = tx.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1 FOR UPDATE", email).Scan(&learnerID)
	switch {
	case err == nil:
		var bound bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM auth_provider_identities WHERE provider='clerk' AND learner_id=$1::uuid)", learnerID).Scan(&bound); err != nil {
			return "", err
		}
		if bound {
			return "", errSSOLinking
		}
	case errors.Is(err, pgx.ErrNoRows):
		name := strings.TrimSpace(identity.Name)
		if name == "" || len(name) > 160 || strings.ContainsAny(name, "\x00\r\n") {
			name = "Learner"
		}
		if err = tx.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES($1) RETURNING id::text", name).Scan(&learnerID); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO auth_identities(email,learner_id) VALUES($1,$2::uuid)", email, learnerID); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO learner_profiles(learner_id,locale,time_zone,analytics_opt_out) VALUES($1::uuid,$2,$3,$4)`, learnerID, locale, zone, optOut); err != nil {
			return "", err
		}
	default:
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth_provider_identities(provider,subject,learner_id,verified_email) VALUES('clerk',$1,$2::uuid,$3)`, identity.UserID, learnerID, email)
	return learnerID, err
}

func ssoCodeHash(key []byte, code string) string { return keyedHash(key, "sso-code:"+code) }

// ssoMaxCodeTries bounds guesses against one hand-off before the attempt is burned.
const ssoMaxCodeTries = 5

type ssoCompleteInput struct {
	State        string `json:"state"`
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
}

// ssoComplete exchanges a finished handoff for Duolify tokens. Only the holder of the
// PKCE verifier that started the attempt can complete it, and only once.
func (s *Service) ssoComplete(w http.ResponseWriter, r *http.Request) {
	if !s.ssoEnabled() {
		ssoUnavailable(w)
		return
	}
	web, ok := s.webGate(w, r)
	if !ok {
		return
	}
	var in ssoCompleteInput
	if !decode(w, r, &in) {
		return
	}
	if len(in.State) < 32 || len(in.State) > 128 || len(in.Code) < 32 || len(in.Code) > 128 || !validPKCEVerifier(in.CodeVerifier) || !ValidSSORedirect(in.RedirectURI) {
		respond(w, http.StatusBadRequest, map[string]string{"error": "Invalid sign-in request."})
		return
	}
	ctx := r.Context()
	if err := s.ssoThrottle(ctx, "sso-complete-ip:"+s.clientIP(r), s.limits.VerifyIP); err != nil {
		failure(w, err)
		return
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		failure(w, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stateHash := keyedHash(s.oauthKey, in.State)
	var challenge, redirect, learnerID, codeHash string
	var ready bool
	var tries int
	err = tx.QueryRow(ctx, `SELECT code_challenge,redirect_uri,COALESCE(learner_id::text,''),COALESCE(code_hash,''),code_attempts,expires_at>now() AND consumed_at IS NULL AND handed_off_at IS NOT NULL AND code_expires_at>now() FROM auth_oauth_attempts WHERE state_hash=$1 FOR UPDATE`, stateHash).Scan(&challenge, &redirect, &learnerID, &codeHash, &tries, &ready)
	if err != nil || !ready || tries >= ssoMaxCodeTries || codeHash == "" {
		respond(w, http.StatusUnauthorized, map[string]string{"error": "Sign-in could not be verified. Please retry."})
		return
	}
	// Every redemption needs the one-time code (delivered only to the browser that signed
	// in), the PKCE verifier (held only by the app) and the exact redirect.
	codeOK := subtle.ConstantTimeCompare([]byte(codeHash), []byte(ssoCodeHash(s.oauthKey, in.Code))) == 1
	pkceOK := subtle.ConstantTimeCompare([]byte(challenge), []byte(PKCEChallenge(in.CodeVerifier))) == 1
	if !codeOK || !pkceOK || redirect != in.RedirectURI {
		// Count the failure; the fifth burns the attempt so the code cannot be brute forced.
		burn := "UPDATE auth_oauth_attempts SET code_attempts=code_attempts+1, consumed_at=CASE WHEN code_attempts+1>=$2 THEN now() ELSE consumed_at END WHERE state_hash=$1"
		if _, err = tx.Exec(ctx, burn, stateHash, ssoMaxCodeTries); err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, http.StatusUnauthorized, map[string]string{"error": "Sign-in could not be verified. Please retry."})
		return
	}
	var sessionID string
	var expiry time.Time
	if _, err = tx.Exec(ctx, "UPDATE auth_oauth_attempts SET consumed_at=now(),code_hash=NULL WHERE state_hash=$1", stateHash); err == nil {
		err = tx.QueryRow(ctx, "INSERT INTO auth_sessions(learner_id) VALUES($1::uuid) RETURNING id::text,expires_at", learnerID).Scan(&sessionID, &expiry)
	}
	var tokens Tokens
	if err == nil {
		tokens, err = s.issue(ctx, tx, learnerID, sessionID, expiry)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(w, err)
		return
	}
	respondTokens(w, web, tokens, expiry)
}
