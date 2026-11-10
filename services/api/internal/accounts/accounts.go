// Package accounts implements first-party email code authentication.
package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errCredentials = errors.New("invalid or expired code")
var errLimited = errors.New("rate limit exceeded")
var localePattern = regexp.MustCompile(`^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// Tokens contains credentials delivered once, never persisted in plaintext by the API.
type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// Profile is the authenticated Learner read; no authentication secrets are returned.
type Profile = db.GetAuthenticatedLearnerRow

// Service owns stateless HTTP authentication backed by Postgres.
type Service struct {
	pool          *pgxpool.Pool
	key           []byte
	otpKey        []byte
	oauthKey      []byte
	rateKey       []byte
	sealKey       []byte
	mail          Mailer
	oauth         OAuthProvider
	apple         AppleOAuthProvider
	mux           *http.ServeMux
	profile       ProfileOptions
	limits        RateLimits
	personDeleter PersonDeleter
	setupChat     SetupChatOptions
}

// New requires a shared signing/HMAC secret of at least 32 bytes.
func New(pool *pgxpool.Pool, secret string, mailer Mailer) (*Service, error) {
	return NewWithOAuthProvider(pool, secret, mailer, unavailableOAuthProvider{})
}

// NewWithOAuthProvider injects the provider boundary used by Google and future Apple OAuth.
func NewWithOAuthProvider(pool *pgxpool.Pool, secret string, mailer Mailer, provider OAuthProvider) (*Service, error) {
	return NewWithOAuthProviders(pool, secret, mailer, provider, unavailableAppleProvider{})
}

// NewWithOAuthProviders injects independent Google and Apple adapters. Both default
// to unavailable in production until their respective provisioning gates are met.
func NewWithOAuthProviders(pool *pgxpool.Pool, secret string, mailer Mailer, google OAuthProvider, apple AppleOAuthProvider) (*Service, error) {
	if len(secret) < 32 || mailer == nil {
		return nil, errors.New("auth requires a 32-byte secret, Mailer, and OAuth providers")
	}
	if google == nil {
		google = unavailableOAuthProvider{}
	}
	if apple == nil {
		apple = unavailableAppleProvider{}
	}
	if os.Getenv("DUOLIFY_ENV") != "development" {
		if _, ok := google.(*FixtureGoogleProvider); ok {
			return nil, errors.New("fixture Google provider requires development")
		}
		if _, ok := apple.(*FixtureAppleProvider); ok {
			return nil, errors.New("fixture Apple provider requires development")
		}
	}
	s := &Service{pool: pool, key: authKey(secret, "jwt"), otpKey: authKey(secret, "otp-hash"), oauthKey: authKey(secret, "oauth-hash"), rateKey: authKey(secret, "rate-hash"), sealKey: authKey(secret, "credential-encryption"), mail: mailer, oauth: google, apple: apple, mux: http.NewServeMux(), limits: DefaultRateLimits()}
	s.mux.HandleFunc("POST /v1/auth/code", s.requestCode)
	s.mux.HandleFunc("POST /v1/auth/verify", s.verify)
	s.mux.HandleFunc("POST /v1/auth/google/start", s.googleOAuthStart)
	s.mux.HandleFunc("POST /v1/auth/google/complete", s.googleOAuthComplete)
	s.mux.HandleFunc("POST /v1/auth/apple/start", s.appleOAuthStart)
	s.mux.HandleFunc("POST /v1/auth/apple/complete", s.appleOAuthComplete)
	s.mux.HandleFunc("POST /v1/auth/refresh", s.refresh)
	s.mux.HandleFunc("POST /v1/auth/logout", s.logout)
	s.mux.Handle("GET /v1/auth/sessions", s.Protect(http.HandlerFunc(s.listSessions)))
	s.mux.Handle("DELETE /v1/auth/sessions", s.Protect(http.HandlerFunc(s.revokeSessions)))
	s.mux.Handle("DELETE /v1/auth/sessions/{sessionID}", s.Protect(http.HandlerFunc(s.revokeSession)))
	s.mux.Handle("GET /v1/me", s.Protect(http.HandlerFunc(s.me)))
	s.ConfigureProfile(ProfileOptions{TextLimit: 2000})
	s.registerProfile()
	s.registerPrivacy()
	return s, nil
}
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.mux.ServeHTTP(w, r)
}

// ConfigureRateLimits installs limits validated by RateLimitsFromEnv.
func (s *Service) ConfigureRateLimits(limits RateLimits) {
	s.limits = limits
}

// NormalizeEmail accepts one unadorned mailbox, lowercases it and bounds storage.
func NormalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return "", errors.New("invalid email address")
	}
	return email, nil
}

type codeInput struct {
	Email           string `json:"email"`
	Code            string `json:"code"`
	AgeConfirmed    bool   `json:"age_confirmed"`
	Locale          string `json:"locale"`
	TimeZone        string `json:"time_zone"`
	AnalyticsOptOut bool   `json:"analytics_opt_out"`
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		respond(w, 400, map[string]string{"error": "Invalid request."})
		return false
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		respond(w, 400, map[string]string{"error": "Invalid request."})
		return false
	}
	return true
}
func input(w http.ResponseWriter, r *http.Request) (codeInput, bool) {
	var in codeInput
	if !decode(w, r, &in) {
		return in, false
	}
	email, err := NormalizeEmail(in.Email)
	if err != nil || !in.AgeConfirmed {
		respond(w, 400, map[string]string{"error": "A valid email and confirmation that you are 16 or older are required."})
		return in, false
	}
	in.Email = email
	return in, true
}
func (s *Service) hmac(value string) string {
	return keyedHash(s.otpKey, value)
}
func keyedHash(key []byte, value string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}
func refreshHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (s *Service) limit(ctx context.Context, tx pgx.Tx, key string, maximum int, email ...string) error {
	var count int
	err := tx.QueryRow(ctx, `INSERT INTO auth_rate_limits(key_hash,count,expires_at) VALUES($1,1,now()+interval '1 hour')
 ON CONFLICT(key_hash) DO UPDATE SET count=CASE WHEN auth_rate_limits.expires_at<=now() THEN 1 ELSE auth_rate_limits.count+1 END,
 expires_at=CASE WHEN auth_rate_limits.expires_at<=now() THEN now()+interval '1 hour' ELSE auth_rate_limits.expires_at END RETURNING count`, keyedHash(s.rateKey, key)).Scan(&count)
	if err != nil {
		return err
	}
	if len(email) > 0 && (strings.HasPrefix(key, "request-email-ip:") || strings.HasPrefix(key, "delivery-email:")) {
		if _, err = tx.Exec(ctx, "UPDATE auth_rate_limits SET email_hash=$2 WHERE key_hash=$1", keyedHash(s.rateKey, key), keyedHash(s.rateKey, "delivery-email:"+email[0])); err != nil {
			return err
		}
	}
	if count > maximum {
		return errLimited
	}
	return nil
}
func (s *Service) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if s.limits.TrustedProxyHops <= 0 {
		return host
	}
	chain := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	index := len(chain) - s.limits.TrustedProxyHops
	if index < 0 {
		return host
	}
	candidate := net.ParseIP(strings.TrimSpace(chain[index]))
	if candidate == nil {
		return host
	}
	return candidate.String()
}
func (s *Service) requestCode(w http.ResponseWriter, r *http.Request) {
	in, ok := input(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		failure(w, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, v := range []struct {
		key string
		max int
	}{
		{"request-ip:" + s.clientIP(r), s.limits.RequestIP},
		{"request-email-ip:" + in.Email + ":" + s.clientIP(r), s.limits.RequestEmail},
	} {
		if err = s.limit(ctx, tx, v.key, v.max, in.Email); err != nil {
			if errors.Is(err, errLimited) {
				err = errors.Join(err, tx.Commit(ctx))
			}
			failure(w, err)
			return
		}
	}
	// Serialize even the first insert, when no code row exists to lock yet.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", in.Email); err != nil {
		failure(w, err)
		return
	}
	if _, err = tx.Exec(ctx, `DELETE FROM auth_codes WHERE expires_at<now(); DELETE FROM auth_rate_limits WHERE expires_at<now(); DELETE FROM auth_sessions WHERE expires_at<now(); UPDATE auth_refresh_tokens SET successor_ciphertext=NULL WHERE consumed_at<now()-interval '5 seconds' AND successor_ciphertext IS NOT NULL`); err != nil {
		failure(w, err)
		return
	}
	var sealed []byte
	var active, cooldown bool
	err = tx.QueryRow(ctx, `SELECT code_ciphertext,expires_at>now() AND consumed_at IS NULL,last_sent_at>now()-interval '60 seconds' FROM auth_codes WHERE email=$1 FOR UPDATE`, in.Email).Scan(&sealed, &active, &cooldown)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		failure(w, err)
		return
	}
	if active && cooldown {
		if err = tx.Commit(ctx); err != nil {
			failure(w, err)
			return
		}
		respond(w, 202, map[string]int{"expires_in": 600})
		return
	}
	// Email-wide delivery throttling is silent and independent of the caller's
	// budget. It never replaces an active code or prevents its verification.
	if err = s.limit(ctx, tx, "delivery-email:"+in.Email, s.limits.emailDeliveries(), in.Email); err != nil {
		if errors.Is(err, errLimited) {
			if err = tx.Commit(ctx); err == nil {
				respond(w, 202, map[string]int{"expires_in": 600})
				return
			}
		}
		failure(w, err)
		return
	}
	var code, hash string
	if active {
		raw, openErr := s.unseal(sealed, "otp:"+in.Email)
		if openErr != nil {
			failure(w, openErr)
			return
		}
		code = string(raw)
		hash = s.hmac(in.Email + ":" + code)
		_, err = tx.Exec(ctx, "UPDATE auth_codes SET last_sent_at=now() WHERE email=$1", in.Email)
	} else {
		n, randErr := rand.Int(rand.Reader, big.NewInt(900000))
		if randErr != nil {
			failure(w, randErr)
			return
		}
		code = fmt.Sprintf("%06d", n.Int64()+100000)
		hash = s.hmac(in.Email + ":" + code)
		sealed, err = s.seal([]byte(code), "otp:"+in.Email)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO auth_codes(email,code_hash,code_ciphertext,last_sent_at,expires_at) VALUES($1,$2,$3,now(),now()+interval '10 minutes')
 ON CONFLICT(email) DO UPDATE SET code_hash=$2,code_ciphertext=$3,last_sent_at=now(),expires_at=now()+interval '10 minutes',attempts=0,consumed_at=NULL`, in.Email, hash, sealed)
		}
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(w, err)
		return
	}
	if err = s.mail.SendCode(ctx, in.Email, code); err != nil {
		// Preserve the code and attempt count after transient mail failure; allow retry.
		_, _ = s.pool.Exec(ctx, "UPDATE auth_codes SET last_sent_at=now()-interval '60 seconds' WHERE email=$1 AND code_hash=$2", in.Email, hash)
		failure(w, err)
		return
	}
	respond(w, 202, map[string]int{"expires_in": 600})
}
func (s *Service) verify(w http.ResponseWriter, r *http.Request) {
	in, ok := input(w, r)
	if !ok {
		return
	}
	if in.Locale == "" {
		in.Locale = "en"
	}
	if in.TimeZone == "" {
		in.TimeZone = "UTC"
	}
	if _, err := time.LoadLocation(in.TimeZone); err != nil || len(in.TimeZone) > 64 || !localePattern.MatchString(in.Locale) || len(in.Locale) > 64 {
		respond(w, 400, map[string]string{"error": "Invalid locale or time zone."})
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		failure(w, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = s.limit(ctx, tx, "verify-ip:"+s.clientIP(r), s.limits.VerifyIP); err != nil {
		if errors.Is(err, errLimited) {
			err = errors.Join(err, tx.Commit(ctx))
		}
		failure(w, err)
		return
	}
	var hash string
	var active bool
	var attempts int
	err = tx.QueryRow(ctx, `SELECT code_hash,expires_at>now() AND consumed_at IS NULL,attempts FROM auth_codes WHERE email=$1 FOR UPDATE`, in.Email).Scan(&hash, &active, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			failure(w, err)
			return
		}
		failure(w, errCredentials)
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	if attempts < 5 {
		_, err = tx.Exec(ctx, "UPDATE auth_codes SET attempts=attempts+1 WHERE email=$1", in.Email)
		if err != nil {
			failure(w, err)
			return
		}
	}
	equal := subtle.ConstantTimeCompare([]byte(hash), []byte(s.hmac(in.Email+":"+in.Code))) == 1
	if !active || attempts >= 5 || len(in.Code) != 6 || !equal {
		if err = tx.Commit(ctx); err != nil {
			failure(w, err)
			return
		}
		failure(w, errCredentials)
		return
	}
	// Row lock serializes consumption; identity uniqueness also protects separate codes.
	_, err = tx.Exec(ctx, "UPDATE auth_codes SET consumed_at=now() WHERE email=$1", in.Email)
	if err != nil {
		failure(w, err)
		return
	}
	var learnerID string
	err = tx.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", in.Email).Scan(&learnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('Learner') RETURNING id::text").Scan(&learnerID)
		if err == nil {
			_, err = tx.Exec(ctx, "INSERT INTO auth_identities(email,learner_id) VALUES($1,$2::uuid)", in.Email, learnerID)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO learner_profiles(learner_id,locale,time_zone,analytics_opt_out) VALUES($1::uuid,$2,$3,$4)`, learnerID, in.Locale, in.TimeZone, in.AnalyticsOptOut)
		}
	}
	if err != nil {
		failure(w, err)
		return
	}
	if in.AnalyticsOptOut {
		_, err = tx.Exec(ctx, "UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id=$1::uuid", learnerID)
		if err != nil {
			failure(w, err)
			return
		}
	}
	var sessionID string
	var expiry time.Time
	err = tx.QueryRow(ctx, "INSERT INTO auth_sessions(learner_id) VALUES($1::uuid) RETURNING id::text,expires_at", learnerID).Scan(&sessionID, &expiry)
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
	respond(w, 200, tokens)
}
func (s *Service) issue(ctx context.Context, tx pgx.Tx, learnerID, sessionID string, expiry time.Time) (Tokens, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return Tokens{}, err
	}
	secret := base64.RawURLEncoding.EncodeToString(bytes[:])
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Issuer: "duolify", Audience: jwt.ClaimStrings{"duolify-client"}, Subject: learnerID, ID: sessionID, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute))})
	access, err := token.SignedString(s.key)
	if err != nil {
		return Tokens{}, err
	}
	_, err = tx.Exec(ctx, "INSERT INTO auth_refresh_tokens(token_hash,session_id,expires_at) VALUES($1,$2::uuid,$3)", refreshHash(secret), sessionID, expiry)
	return Tokens{access, secret, 900}, err
}

type refreshInput struct {
	RefreshToken string `json:"refresh_token"`
}

func (s *Service) refresh(w http.ResponseWriter, r *http.Request) {
	var in refreshInput
	if !decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		failure(w, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var learnerID, sessionID string
	var active, used bool
	var expiry time.Time
	var withinGrace bool
	var successor []byte
	err = tx.QueryRow(ctx, `SELECT s.learner_id::text,s.id::text,s.revoked_at IS NULL AND s.expires_at>now() AND t.expires_at>now(),t.consumed_at IS NOT NULL,s.expires_at,COALESCE(t.consumed_at>now()-interval '5 seconds',false),t.successor_ciphertext
 FROM auth_refresh_tokens t JOIN auth_sessions s ON s.id=t.session_id WHERE t.token_hash=$1 FOR UPDATE OF s,t`, refreshHash(in.RefreshToken)).Scan(&learnerID, &sessionID, &active, &used, &expiry, &withinGrace, &successor)
	if errors.Is(err, pgx.ErrNoRows) {
		failure(w, errCredentials)
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	if used && active && withinGrace && len(successor) > 0 {
		raw, openErr := s.unseal(successor, "refresh:"+refreshHash(in.RefreshToken))
		var tokens Tokens
		if openErr == nil {
			openErr = json.Unmarshal(raw, &tokens)
		}
		if openErr != nil {
			failure(w, openErr)
			return
		}
		if err = tx.Commit(ctx); err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, tokens)
		return
	}
	if used || !active {
		if _, err = tx.Exec(ctx, "UPDATE auth_sessions SET revoked_at=now() WHERE id=$1::uuid", sessionID); err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			failure(w, err)
			return
		}
		failure(w, errCredentials)
		return
	}
	_, err = tx.Exec(ctx, "UPDATE auth_refresh_tokens SET consumed_at=now() WHERE token_hash=$1", refreshHash(in.RefreshToken))
	var tokens Tokens
	if err == nil {
		tokens, err = s.issue(ctx, tx, learnerID, sessionID, expiry)
	}
	if err == nil {
		// #nosec G117 -- immediately AES-GCM sealed with token-hash AAD; never persisted or logged in plaintext.
		raw, marshalErr := json.Marshal(tokens)
		err = marshalErr
		if err == nil {
			successor, err = s.seal(raw, "refresh:"+refreshHash(in.RefreshToken))
		}
		if err == nil {
			_, err = tx.Exec(ctx, "UPDATE auth_refresh_tokens SET successor_ciphertext=$2 WHERE token_hash=$1", refreshHash(in.RefreshToken), successor)
		}
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, 200, tokens)
}
func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	var in refreshInput
	if !decode(w, r, &in) {
		return
	}
	_, err := s.pool.Exec(r.Context(), `UPDATE auth_sessions SET revoked_at=now() WHERE id IN(SELECT session_id FROM auth_refresh_tokens WHERE token_hash=$1)`, refreshHash(in.RefreshToken))
	if err != nil {
		failure(w, err)
		return
	}
	w.WriteHeader(204)
}

// ParseAccess validates the signed JWT; Protect also checks live session revocation.
func (s *Service) ParseAccess(raw string) (*jwt.RegisteredClaims, error) {
	claims := new(jwt.RegisteredClaims)
	_, err := jwt.ParseWithClaims(raw, claims, func(_ *jwt.Token) (any, error) { return s.key, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("duolify"), jwt.WithAudience("duolify-client"), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err == nil && (!uuidPattern.MatchString(claims.Subject) || !uuidPattern.MatchString(claims.ID)) {
		err = errCredentials
	}
	return claims, err
}

type learnerKey struct{}
type sessionKey struct{}

// LearnerIDFromContext extracts the authenticated learner UUID from context.
func LearnerIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(learnerKey{}).(string)
	return id, ok && id != ""
}

// SessionIDFromContext extracts the active session UUID from context.
func SessionIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(sessionKey{}).(string)
	return id, ok && id != ""
}

// Protect rejects missing, expired, revoked and deleted-account credentials.
func (s *Service) Protect(next http.Handler) http.Handler {
	return s.protect(next, nil)
}

// ProtectWithDenied invokes the audit hook for missing, invalid or revoked credentials.
func (s *Service) ProtectWithDenied(next http.Handler, denied func(*http.Request)) http.Handler {
	return s.protect(next, denied)
}

func (s *Service) protect(next http.Handler, denied func(*http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			if denied != nil {
				denied(r)
			}
			failure(w, errCredentials)
			return
		}
		claims, err := s.ParseAccess(raw)
		if err != nil {
			if denied != nil {
				denied(r)
			}
			failure(w, errCredentials)
			return
		}
		var active bool
		err = s.pool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM auth_sessions WHERE id=$1::uuid AND learner_id=$2::uuid AND revoked_at IS NULL AND expires_at>now())", claims.ID, claims.Subject).Scan(&active)
		if err != nil {
			failure(w, err)
			return
		}
		if !active {
			if denied != nil {
				denied(r)
			}
			failure(w, errCredentials)
			return
		}
		ctx := context.WithValue(r.Context(), learnerKey{}, claims.Subject)
		ctx = context.WithValue(ctx, sessionKey{}, claims.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (s *Service) me(w http.ResponseWriter, r *http.Request) {
	id, _ := r.Context().Value(learnerKey{}).(string)
	profile, err := db.New(s.pool).GetAuthenticatedLearner(r.Context(), id)
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, 200, profile)
}
func failure(w http.ResponseWriter, err error) {
	status, message := 503, "Sign-in is unavailable. Please retry."
	if errors.Is(err, errCredentials) {
		status, message = 401, "Code is invalid or expired. Request a new code."
	}
	if errors.Is(err, errLimited) {
		status, message = 429, "Too many attempts. Try again in an hour."
		w.Header().Set("Retry-After", "3600")
	}
	respond(w, status, map[string]string{"error": message})
}
func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// LearnerID returns the verified internal UUID supplied by Protect.
func LearnerID(ctx context.Context) string {
	id, _ := ctx.Value(learnerKey{}).(string)
	return id
}

// SessionID returns the verified session UUID set by Protect.
func SessionID(ctx context.Context) string {
	id, _ := ctx.Value(sessionKey{}).(string)
	return id
}
