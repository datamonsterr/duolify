package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TurnstileEmailCode, TurnstileSignUp and TurnstileSharePreview bind proof to its use.
const (
	TurnstileEmailCode    = "email_code"
	TurnstileSignUp       = "sign_up"
	TurnstileSharePreview = "share_preview"
	TurnstileHeader       = "X-Turnstile-Token"

	turnstileSiteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	turnstileTimeout       = 3 * time.Second
	turnstileTokenLimit    = 2048
	turnstileResponseLimit = 16 << 10
)

// TurnstileReason is the internal, content-free result of a verification.
type TurnstileReason string

// TurnstileOK and the denial reasons define the bounded telemetry values.
const (
	TurnstileOK          TurnstileReason = "none"
	TurnstileMissing     TurnstileReason = "missing"
	TurnstileInvalid     TurnstileReason = "invalid"
	TurnstileExpired     TurnstileReason = "expired_or_duplicate"
	TurnstileMismatch    TurnstileReason = "mismatch"
	TurnstileUnavailable TurnstileReason = "provider_unavailable"
)

// TurnstileVerifier validates a proof for one action, using a trusted client IP when supplied.
type TurnstileVerifier interface {
	Verify(ctx context.Context, proof, remoteIP, action string) TurnstileReason
}

type siteverify struct {
	secret, endpoint string
	hostnames        []string
	client           *http.Client
}

// NewSiteverify uses the official endpoint by default. Tests can supply a local endpoint or transport.
func NewSiteverify(secret, endpoint string, hostnames []string, client *http.Client) TurnstileVerifier {
	if endpoint == "" {
		endpoint = turnstileSiteverifyURL
	}
	configured := http.Client{Timeout: turnstileTimeout / 2}
	if client != nil {
		configured = *client
	}
	configured.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &siteverify{secret: secret, endpoint: endpoint, hostnames: slices.Clone(hostnames), client: &configured}
}

type siteverifyInput struct {
	Secret         string `json:"secret"`
	Response       string `json:"response"`
	RemoteIP       string `json:"remoteip,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (v *siteverify) Verify(ctx context.Context, proof, remoteIP, action string) TurnstileReason {
	switch {
	case strings.TrimSpace(proof) == "":
		return TurnstileMissing
	case len(proof) > turnstileTokenLimit:
		return TurnstileInvalid
	case ctx.Err() != nil:
		return TurnstileUnavailable
	}
	if net.ParseIP(remoteIP) == nil {
		remoteIP = ""
	}
	key, err := uuid.NewRandom()
	if err != nil {
		return TurnstileUnavailable
	}
	// #nosec G117 -- Siteverify requires the runtime secret in the JSON body over HTTPS.
	payload, err := json.Marshal(siteverifyInput{Secret: v.secret, Response: proof, RemoteIP: remoteIP, IdempotencyKey: key.String()})
	if err != nil {
		return TurnstileUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, turnstileTimeout)
	defer cancel()
	result, retry := v.attempt(ctx, payload, action)
	if retry && ctx.Err() == nil {
		result, _ = v.attempt(ctx, payload, action)
	}
	return result
}

func (v *siteverify) attempt(ctx context.Context, payload []byte, action string) (TurnstileReason, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, bytes.NewReader(payload))
	if err != nil {
		return TurnstileUnavailable, false
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := v.client.Do(req)
	if err != nil {
		return TurnstileUnavailable, true
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return TurnstileUnavailable, false
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, turnstileResponseLimit+1))
	if err != nil {
		return TurnstileUnavailable, true
	}
	var result struct {
		Success  *bool    `json:"success"`
		Hostname string   `json:"hostname"`
		Action   string   `json:"action"`
		Errors   []string `json:"error-codes"`
	}
	if len(data) > turnstileResponseLimit || json.Unmarshal(data, &result) != nil || result.Success == nil {
		return TurnstileUnavailable, false
	}
	if !*result.Success {
		for _, code := range result.Errors {
			switch code {
			case "internal-error", "missing-input-secret", "invalid-input-secret":
				return TurnstileUnavailable, false
			case "timeout-or-duplicate":
				return TurnstileExpired, false
			}
		}
		return TurnstileInvalid, false
	}
	if result.Action != action || !slices.Contains(v.hostnames, strings.ToLower(result.Hostname)) {
		return TurnstileMismatch, false
	}
	return TurnstileOK, false
}

// Turnstile is the shared origin gate. A nil gate leaves the existing flow enabled without a challenge.
type Turnstile struct {
	siteKey  string
	verifier TurnstileVerifier
	events   analytics.Events
}

// NewTurnstile wires the public site key, verification and consent-aware events.
func NewTurnstile(siteKey string, verifier TurnstileVerifier, events analytics.Events) *Turnstile {
	return &Turnstile{siteKey: siteKey, verifier: verifier, events: events}
}

var turnstileHostname = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$`)

// TurnstileFromEnv validates the enabled configuration before the API serves requests.
func TurnstileFromEnv(getenv func(string) string, events analytics.Events) (*Turnstile, error) {
	flag := strings.TrimSpace(getenv("TURNSTILE_ENABLED"))
	if flag == "" {
		return nil, nil
	}
	enabled, err := strconv.ParseBool(flag)
	if err != nil {
		return nil, errors.New("TURNSTILE_ENABLED must be a boolean")
	}
	if !enabled {
		return nil, nil
	}
	siteKey := strings.TrimSpace(getenv("TURNSTILE_SITE_KEY"))
	secret := strings.TrimSpace(getenv("TURNSTILE_SECRET_KEY"))
	if siteKey == "" || secret == "" {
		return nil, errors.New("enabled Turnstile requires TURNSTILE_SITE_KEY and TURNSTILE_SECRET_KEY")
	}
	hostnames := strings.Split(getenv("TURNSTILE_ALLOWED_HOSTNAMES"), ",")
	for i, hostname := range hostnames {
		hostname = strings.ToLower(strings.TrimSpace(hostname))
		if len(hostname) > 253 || !turnstileHostname.MatchString(hostname) {
			return nil, errors.New("enabled Turnstile requires exact hosts in TURNSTILE_ALLOWED_HOSTNAMES")
		}
		hostnames[i] = hostname
	}
	return NewTurnstile(siteKey, NewSiteverify(secret, "", hostnames, nil), events), nil
}

// Require verifies proof once for this request and emits only bounded outcomes.
// The optional learner ID must come from an authenticated request context.
func (t *Turnstile) Require(w http.ResponseWriter, r *http.Request, proof, remoteIP, action, learnerID string) bool {
	if t == nil {
		return true
	}
	reason := TurnstileUnavailable
	if t.verifier != nil {
		reason = t.verifier.Verify(r.Context(), proof, remoteIP, action)
	}
	telemetry.FromContext(r.Context()).RecordTurnstile(action, turnstileOutcome(reason), string(reason))
	t.capture(learnerID, action, reason)
	if reason == TurnstileOK {
		return true
	}
	respond(w, http.StatusBadRequest, map[string]string{"code": "turnstile_failed", "error": "Security check failed. Try again."})
	return false
}

func turnstileOutcome(reason TurnstileReason) string {
	switch reason {
	case TurnstileOK:
		return "passed"
	case TurnstileUnavailable:
		return "unavailable"
	default:
		return "rejected"
	}
}

func (t *Turnstile) capture(learnerID, action string, reason TurnstileReason) {
	if t == nil || t.events == nil || !uuidPattern.MatchString(learnerID) || learnerID == "00000000-0000-4000-8000-000000000011" {
		return
	}
	_ = t.events.Capture(learnerID, "turnstile_verification", map[string]any{
		"source": "api", "platform": "web", "app_version": "0.1.0",
		"surface": action, "outcome": turnstileOutcome(reason), "reason_code": string(reason),
	})
}

// ConfigureTurnstile sets the shared challenge gate at service startup.
func (s *Service) ConfigureTurnstile(gate *Turnstile) { s.turnstile = gate }

// Keep the existing limiter ahead of the provider call. In enabled mode, commit
// those counters first so rejected proofs spend the budget and the network wait
// holds no database transaction. OTP mutations start in a new transaction.
func (s *Service) requireCodeProof(w http.ResponseWriter, r *http.Request, tx pgx.Tx, proof, action string) (pgx.Tx, bool) {
	if s.turnstile == nil {
		return tx, true
	}
	ctx := r.Context()
	if err := tx.Commit(ctx); err != nil {
		failure(w, err)
		return tx, false
	}
	if !s.turnstile.Require(w, r, proof, s.clientIP(r), action, "") {
		return tx, false
	}
	next, err := s.pool.Begin(ctx)
	if err != nil {
		failure(w, err)
		return tx, false
	}
	return next, true
}

func (s *Service) turnstileConfig(w http.ResponseWriter, _ *http.Request) {
	if s.turnstile == nil {
		respond(w, http.StatusOK, map[string]bool{"enabled": false})
		return
	}
	respond(w, http.StatusOK, struct {
		Enabled bool   `json:"enabled"`
		SiteKey string `json:"site_key"`
	}{true, s.turnstile.siteKey})
}
