package pricing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const systemActor = "00000000-0000-4000-8000-000000000011"

var pricingUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// Admin implements the protected pricing draft lifecycle. The router must wrap
// it with accounts.Protect; it independently checks role and recent auth so
// denied access can be recorded in the same durable audit store.
type Admin struct {
	pool     *pgxpool.Pool
	events   analytics.Events
	owner    string
	verifier ExternalVerifier
}

type cliActorKey struct{}

// SetVerifier injects the provider mapping and pricing verifier.
func (a *Admin) SetVerifier(v ExternalVerifier) {
	a.verifier = v
}

// RecordDenied records a route rejected by auth before the pricing handler ran.
func (a *Admin) RecordDenied(r *http.Request) {
	a.denied(r, systemActor, "authentication required")
}

// NewAdmin creates the pricing administrator service over Postgres and analytics.
func NewAdmin(pool *pgxpool.Pool, events analytics.Events, ownerID string) *Admin {
	return &Admin{pool: pool, events: events, owner: strings.ToLower(ownerID)}
}

type adminInput struct {
	Config         json.RawMessage `json:"config"`
	Reason         string          `json:"reason"`
	OverrideReason string          `json:"override_reason,omitempty"`
}

type reasonInput struct {
	Reason         string `json:"reason"`
	OverrideReason string `json:"override_reason,omitempty"`
}

type rollbackInput struct {
	Version        string    `json:"version"`
	EffectiveFrom  time.Time `json:"effective_from"`
	Reason         string    `json:"reason"`
	OverrideReason string    `json:"override_reason,omitempty"`
}

func validateReason(reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) == 0 || len(reason) > 1000 {
		return errors.New("reason must contain 1 to 1000 characters")
	}
	return nil
}

func normalizeEffectiveFrom(raw []byte) ([]byte, Config, error) {
	c, err := Parse(raw)
	if err != nil {
		return nil, Config{}, err
	}
	var document map[string]json.RawMessage
	if err = json.Unmarshal(raw, &document); err != nil {
		return nil, Config{}, err
	}
	canonical, err := json.Marshal(c.EffectiveFrom.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano))
	if err != nil {
		return nil, Config{}, err
	}
	document["effective_from"] = canonical
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, Config{}, err
	}
	c, err = Parse(encoded)
	return encoded, c, err
}

func actorHash(actor string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(actor)))
	return hex.EncodeToString(sum[:])
}

func (a *Admin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor := accounts.LearnerID(r.Context())
	cliActor, _ := r.Context().Value(cliActorKey{}).(string)
	if actor == "" && cliActor != "" {
		actor = cliActor
	}
	if actor == "" {
		a.denied(r, systemActor, "authentication required")
		return
	}
	if !pricingUUID.MatchString(actor) {
		a.denied(r, systemActor, "invalid actor")
		return
	}
	var admin, fresh bool
	var err error
	if cliActor != "" {
		err = a.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM pricing_admin_grants WHERE account_id=$1::uuid)`, actor).Scan(&admin)
		fresh = true
	} else {
		admin, err = HasAdminAccess(r.Context(), a.pool, actor, accounts.SessionID(r.Context()))
		fresh = admin
	}
	if err != nil {
		adminError(w, http.StatusServiceUnavailable, "pricing authorization unavailable")
		return
	}
	if !admin || !fresh {
		a.denied(r, actor, "pricing_admin role and recent authentication required")
		adminError(w, http.StatusForbidden, "pricing administrator access required")
		return
	}
	if r.URL.Path == "/admin/v1/pricing/drafts" && r.Method == http.MethodPost {
		a.create(w, r, actor)
		return
	}
	if r.URL.Path == "/admin/v1/pricing/drafts" && r.Method == http.MethodGet {
		a.list(w, r, actor)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/v1/pricing/drafts/"), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		adminError(w, http.StatusNotFound, "pricing operation not found")
		return
	}
	version, err := url.PathUnescape(parts[0])
	if err != nil || len(version) > 100 {
		adminError(w, http.StatusBadRequest, "invalid pricing version")
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		a.show(w, r, actor, version)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodPatch {
		a.edit(w, r, actor, version)
		return
	}
	if len(parts) != 2 || r.Method != http.MethodPost {
		adminError(w, http.StatusNotFound, "pricing operation not found")
		return
	}
	if parts[1] == "rollback" {
		a.rollback(w, r, actor, version)
		return
	}
	a.action(w, r, actor, version, parts[1])
}

// HasAdminAccess checks the shared HTTP admin grant and recent authentication policy.
// The caller must first validate the session with accounts.Protect.
func HasAdminAccess(ctx context.Context, pool *pgxpool.Pool, actor, sessionID string) (bool, error) {
	var admin, fresh bool
	err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pricing_admin_grants WHERE account_id=$1::uuid),
EXISTS(SELECT 1 FROM auth_sessions WHERE id=$2::uuid AND learner_id=$1::uuid AND revoked_at IS NULL AND expires_at>now() AND reauthenticated_at>=now()-interval '15 minutes')`, actor, sessionID).Scan(&admin, &fresh)
	return admin && fresh, err
}

// ExecuteCLI uses the same handlers and audit operations as HTTP. The actor
// still needs a database grant; only interactive session freshness is skipped.
func (a *Admin) ExecuteCLI(ctx context.Context, actor, method, path string, body []byte) (int, []byte) {
	ctx = context.WithValue(ctx, cliActorKey{}, actor)
	r := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}

func (a *Admin) denied(r *http.Request, actor, reason string) {
	version := r.PathValue("version")
	_, _ = a.pool.Exec(r.Context(), `INSERT INTO pricing_audit(actor_id,action,version,reason,request_id)
VALUES($1,'access_denied',NULLIF($2,''),$3,$4)`, actorHash(actor), version, reason, requestID(r))
}

func requestID(r *http.Request) string {
	id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if id == "" {
		id = "http-" + fmt.Sprintf("%x", time.Now().UnixNano())
	}
	if len(id) > 200 {
		return id[:200]
	}
	return id
}

func adminError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func adminJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeAdmin(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		adminError(w, http.StatusBadRequest, "invalid pricing request")
		return false
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		adminError(w, http.StatusBadRequest, "invalid pricing request")
		return false
	}
	return true
}

func (a *Admin) create(w http.ResponseWriter, r *http.Request, actor string) {
	var in adminInput
	if !decodeAdmin(w, r, &in) {
		a.auditFailure(r.Context(), actor, "validation_failed", "", "invalid request", "{}", requestID(r))
		return
	}
	if err := validateReason(in.Reason); err != nil {
		a.auditFailure(r.Context(), actor, "validation_failed", "", in.Reason, "{}", requestID(r))
		adminError(w, 400, err.Error())
		return
	}
	normalized, c, err := normalizeEffectiveFrom(in.Config)
	in.Config = normalized
	if err != nil {
		a.auditFailure(r.Context(), actor, "validation_failed", "", in.Reason, "{}", requestID(r))
		adminError(w, 422, "invalid pricing config")
		return
	}
	if err = ValidateEconomicsWithContext(r.Context(), in.Config, nil, in.OverrideReason); err != nil {
		a.auditFailure(r.Context(), actor, "validation_failed", c.Version, in.Reason, "{}", requestID(r))
		a.emit(r.Context(), actor, c.Version, "pricing_version_rejected", "")
		adminError(w, 422, err.Error())
		return
	}
	ctx := r.Context()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		adminError(w, 503, "pricing store unavailable")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var parent string
	var previous []byte
	err = tx.QueryRow(ctx, `SELECT version,config FROM pricing_versions WHERE approved AND effective_from<=now() AND NOT EXISTS (SELECT 1 FROM pricing_version_cancellations c WHERE c.version=pricing_versions.version) ORDER BY effective_from DESC LIMIT 1 FOR SHARE`).Scan(&parent, &previous)
	if err != nil {
		adminError(w, 503, "active pricing unavailable")
		return
	}
	var exists bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pricing_versions WHERE version=$1) OR EXISTS(SELECT 1 FROM pricing_drafts WHERE version=$1)", c.Version).Scan(&exists); err != nil || exists {
		a.auditFailure(ctx, actor, "version_create_denied", c.Version, in.Reason, "{}", requestID(r))
		adminError(w, 409, "pricing version already exists")
		return
	}
	adverse, err := classifyAdverse(previous, in.Config)
	if err != nil {
		adminError(w, 422, "unable to classify pricing change")
		return
	}
	class := "non_adverse"
	if adverse {
		class = "adverse"
	}
	summary, err := diffSummary(previous, in.Config)
	if err != nil {
		adminError(w, 422, "pricing diff unavailable")
		return
	}
	_, err = tx.Exec(ctx, `INSERT INTO pricing_drafts(version,parent_version,config,created_by,change_class)
VALUES($1,$2,$3::jsonb,$4,$5)`, c.Version, parent, in.Config, actor, class)
	if err == nil {
		err = insertAudit(ctx, tx, actor, "version_created", c.Version, class, in.Reason, requestID(r), summary)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		adminError(w, 409, "could not create pricing draft")
		return
	}
	a.emit(ctx, actor, c.Version, "pricing_version_created", class)
	adminJSON(w, http.StatusCreated, map[string]any{"version": c.Version, "status": "draft", "change_class": class})
}

func (a *Admin) edit(w http.ResponseWriter, r *http.Request, actor, version string) {
	var in adminInput
	if !decodeAdmin(w, r, &in) {
		a.auditFailure(r.Context(), actor, "validation_failed", version, "invalid request", "{}", requestID(r))
		return
	}
	if err := validateReason(in.Reason); err != nil {
		a.auditFailure(r.Context(), actor, "validation_failed", version, in.Reason, "{}", requestID(r))
		adminError(w, 400, err.Error())
		return
	}
	normalized, c, err := normalizeEffectiveFrom(in.Config)
	in.Config = normalized
	if err != nil || c.Version != version {
		a.auditFailure(r.Context(), actor, "validation_failed", version, in.Reason, "{}", requestID(r))
		adminError(w, 422, "invalid pricing config")
		return
	}
	if err = ValidateEconomicsWithContext(r.Context(), in.Config, a.verifier, in.OverrideReason); err != nil {
		a.auditFailure(r.Context(), actor, "validation_failed", version, in.Reason, "{}", requestID(r))
		a.emit(r.Context(), actor, version, "pricing_version_rejected", "")
		adminError(w, 422, err.Error())
		return
	}
	ctx := r.Context()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		adminError(w, 503, "pricing store unavailable")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var previous []byte
	var author string
	var status string
	err = tx.QueryRow(ctx, `SELECT d.config,d.created_by,d.status FROM pricing_drafts d WHERE d.version=$1 FOR UPDATE`, version).Scan(&previous, &author, &status)
	if err != nil {
		a.auditFailure(r.Context(), actor, "edit_denied", version, in.Reason, "{}", requestID(r))
		var approved bool
		if queryErr := a.pool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM pricing_versions WHERE version=$1)", version).Scan(&approved); queryErr == nil && approved {
			adminError(w, 409, "approved pricing versions are immutable; create a new draft")
		} else {
			adminError(w, 404, "pricing draft not found")
		}
		return
	}
	if status == "approved" {
		a.auditFailure(r.Context(), actor, "edit_denied", version, in.Reason, "{}", requestID(r))
		adminError(w, 409, "approved pricing versions are immutable; create a new draft")
		return
	}
	if author != actor || status != "draft" {
		a.auditFailure(r.Context(), actor, "edit_denied", version, in.Reason, "{}", requestID(r))
		adminError(w, 409, "only the author may edit an unsubmitted draft")
		return
	}
	var active []byte
	if err = tx.QueryRow(ctx, `SELECT config FROM pricing_versions WHERE approved AND effective_from<=now() AND NOT EXISTS (SELECT 1 FROM pricing_version_cancellations c WHERE c.version=pricing_versions.version) ORDER BY effective_from DESC LIMIT 1`).Scan(&active); err != nil {
		adminError(w, 503, "active pricing unavailable")
		return
	}
	adverse, err := classifyAdverse(active, in.Config)
	if err != nil {
		adminError(w, 422, "unable to classify pricing change")
		return
	}
	class := "non_adverse"
	if adverse {
		class = "adverse"
	}
	summary, err := diffSummary(active, in.Config)
	if err != nil {
		adminError(w, 422, "pricing diff unavailable")
		return
	}
	_, err = tx.Exec(ctx, `UPDATE pricing_drafts SET config=$2::jsonb,change_class=$3 WHERE version=$1`, version, in.Config, class)
	if err == nil {
		err = insertAudit(ctx, tx, actor, "version_edited", version, class, in.Reason, requestID(r), summary)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		adminError(w, 409, "could not edit pricing draft")
		return
	}
	adminJSON(w, 200, map[string]any{"version": version, "status": "draft", "change_class": class})
}

func (a *Admin) action(w http.ResponseWriter, r *http.Request, actor, version, action string) {
	var in reasonInput
	if !decodeAdmin(w, r, &in) {
		a.auditFailure(r.Context(), actor, "validation_failed", version, "invalid request", "{}", requestID(r))
		return
	}
	if err := validateReason(in.Reason); err != nil {
		a.auditFailure(r.Context(), actor, "validation_failed", version, in.Reason, "{}", requestID(r))
		adminError(w, 400, err.Error())
		return
	}
	if action == "cancel" {
		a.cancel(w, r, actor, version, in.Reason)
		return
	}
	if action != "validate" && action != "diff" && action != "submit" && action != "approve" && action != "schedule" && action != "reject" {
		adminError(w, 404, "pricing operation not found")
		return
	}
	ctx := r.Context()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		adminError(w, 503, "pricing store unavailable")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var cfg, previous []byte
	var author, status, parent, class string
	err = tx.QueryRow(ctx, `SELECT d.config,d.created_by::text,d.status,d.parent_version,d.change_class,p.config FROM pricing_drafts d JOIN pricing_versions p ON p.version=d.parent_version WHERE d.version=$1 FOR UPDATE OF d`, version).Scan(&cfg, &author, &status, &parent, &class, &previous)
	if err != nil {
		a.auditFailure(ctx, actor, action+"_denied", version, in.Reason, "{}", requestID(r))
		adminError(w, 404, "pricing draft not found")
		return
	}
	parsed, parseErr := Parse(cfg)
	if parseErr != nil {
		a.auditFailure(ctx, actor, "validation_failed", version, in.Reason, "{}", requestID(r))
		adminError(w, 422, "invalid pricing config")
		return
	}
	if action != "diff" {
		if err = ValidateEconomicsWithContext(ctx, cfg, a.verifier, in.OverrideReason); err != nil {
			a.auditFailure(ctx, actor, "validation_failed", version, in.Reason, "{}", requestID(r))
			a.emit(ctx, actor, version, "pricing_version_rejected", class)
			adminError(w, 422, err.Error())
			return
		}
	}
	var activeConfig []byte
	if err = tx.QueryRow(ctx, `SELECT config FROM pricing_versions WHERE approved AND effective_from<=now() AND NOT EXISTS (SELECT 1 FROM pricing_version_cancellations c WHERE c.version=pricing_versions.version) ORDER BY effective_from DESC LIMIT 1 FOR SHARE`).Scan(&activeConfig); err != nil {
		adminError(w, 503, "active pricing unavailable")
		return
	}
	adverse, err := classifyAdverse(activeConfig, cfg)
	if err != nil {
		adminError(w, 422, "unable to classify pricing change")
		return
	}
	currentClass := "non_adverse"
	if adverse {
		currentClass = "adverse"
	}
	if currentClass != class {
		if _, err = tx.Exec(ctx, "UPDATE pricing_drafts SET change_class=$2 WHERE version=$1", version, currentClass); err != nil {
			adminError(w, 503, "pricing classification unavailable")
			return
		}
		class = currentClass
	}
	if action == "validate" || action == "diff" {
		summary, diffErr := diffSummary(previous, cfg)
		if diffErr != nil {
			adminError(w, 422, "pricing diff unavailable")
			return
		}
		if action == "validate" {
			err = insertAudit(ctx, tx, actor, "validation_passed", version, class, in.Reason, requestID(r), summary)
		} else {
			err = insertAudit(ctx, tx, actor, "diff_viewed", version, class, in.Reason, requestID(r), summary)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			adminError(w, 503, "audit write failed")
			return
		}
		if action == "validate" {
			adminJSON(w, 200, map[string]any{"valid": true, "version": version, "change_class": class, "margin_floor_percent": 70})
		} else {
			paths, diffErr := changedPaths(previous, cfg)
			if diffErr != nil {
				adminError(w, 422, "pricing diff unavailable")
				return
			}
			adminJSON(w, 200, map[string]any{"version": version, "parent_version": parent, "change_class": class, "changed_fields": paths})
		}
		return
	}
	if action == "submit" {
		if status != "draft" {
			a.auditFailure(ctx, actor, "submission_denied", version, in.Reason, "{}", requestID(r))
			adminError(w, 409, "only drafts can be submitted")
			return
		}
		_, err = tx.Exec(ctx, `UPDATE pricing_drafts SET status='submitted',submitted_at=now() WHERE version=$1`, version)
		if err == nil {
			err = insertAudit(ctx, tx, actor, "version_submitted", version, class, in.Reason, requestID(r), `{"submitted":true}`)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			adminError(w, 409, "could not submit pricing draft")
			return
		}
		adminJSON(w, 200, map[string]string{"version": version, "status": "submitted"})
		return
	}
	if action == "reject" {
		if status != "submitted" {
			a.auditFailure(ctx, actor, "rejection_denied", version, in.Reason, "{}", requestID(r))
			adminError(w, 409, "only submitted drafts can be rejected")
			return
		}
		_, err = tx.Exec(ctx, `UPDATE pricing_drafts SET status='rejected',reviewed_by=$2,reviewed_at=now() WHERE version=$1`, version, actor)
		if err == nil {
			err = insertAudit(ctx, tx, actor, "version_rejected", version, class, in.Reason, requestID(r), `{"rejected":true}`)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			adminError(w, 409, "could not reject pricing draft")
			return
		}
		a.emit(ctx, actor, version, "pricing_version_rejected", class)
		adminJSON(w, 200, map[string]string{"version": version, "status": "rejected"})
		return
	}
	if status != "submitted" {
		a.auditFailure(ctx, actor, "approval_denied", version, in.Reason, "{}", requestID(r))
		adminError(w, 409, "only submitted drafts can be approved")
		return
	}
	if class == "adverse" {
		if author == actor {
			var adminCount int
			if err = tx.QueryRow(ctx, "SELECT count(*) FROM pricing_admin_grants").Scan(&adminCount); err != nil {
				adminError(w, 503, "pricing role list unavailable")
				return
			}
			if actor != a.owner || adminCount > 1 {
				a.auditFailure(ctx, actor, "approval_denied", version, in.Reason, "{}", requestID(r))
				adminError(w, 403, "adverse drafts require a different pricing administrator")
				return
			}
		}
		var noticeHours int
		var config map[string]any
		if err = json.Unmarshal(cfg, &config); err != nil {
			adminError(w, 422, "invalid pricing config")
			return
		}
		limits, _ := config["limits"].(map[string]any)
		noticeHours, _ = limits["adverse_notice_hours"].(int)
		// JSON decoding above uses float64 for numbers.
		if noticeHours == 0 {
			if n, ok := limits["adverse_notice_hours"].(float64); ok {
				noticeHours = int(n)
			}
		}
		if noticeHours < 72 {
			noticeHours = 72
		}
		var databaseNow time.Time
		if err = tx.QueryRow(ctx, "SELECT now()").Scan(&databaseNow); err != nil {
			adminError(w, 503, "database clock unavailable")
			return
		}
		if parsed.EffectiveFrom.Before(databaseNow.Add(time.Duration(noticeHours) * time.Hour)) {
			a.auditFailure(ctx, actor, "approval_denied", version, in.Reason, "{}", requestID(r))
			adminError(w, 409, "adverse pricing requires the full notice window")
			return
		}
	}
	if err = ValidateEconomicsWithContext(ctx, cfg, a.verifier, in.OverrideReason); err != nil {
		adminError(w, 422, err.Error())
		return
	}
	if action == "schedule" {
		var databaseNow time.Time
		if err = tx.QueryRow(ctx, "SELECT now()").Scan(&databaseNow); err != nil {
			adminError(w, 503, "database clock unavailable")
			return
		}
		if !parsed.EffectiveFrom.After(databaseNow) {
			a.auditFailure(ctx, actor, "schedule_denied", version, in.Reason, "{}", requestID(r))
			adminError(w, 409, "scheduled pricing must have a future effective instant")
			return
		}
		var duplicate bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pricing_versions WHERE effective_from=$1 AND NOT EXISTS (SELECT 1 FROM pricing_version_cancellations c WHERE c.version=pricing_versions.version))`, parsed.EffectiveFrom).Scan(&duplicate); err != nil {
			adminError(w, 503, "schedule availability unavailable")
			return
		}
		if duplicate {
			a.auditFailure(ctx, actor, "schedule_denied", version, in.Reason, "{}", requestID(r))
			adminError(w, 409, "another pricing version already uses this effective instant")
			return
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO pricing_versions(version,effective_from,approved,config) VALUES($1,$2,true,$3::jsonb)`, version, parsed.EffectiveFrom, cfg)
	if err == nil {
		evidenceJSON, _ := json.Marshal(map[string]any{
			"margin_floor_verified": true,
			"economics_verified":    true,
			"mappings_verified":     true,
			"models":                OfficialModelCatalogueEvidence(),
		})
		_, _ = tx.Exec(ctx, `INSERT INTO pricing_verification_evidence(version, evidence) VALUES($1, $2::jsonb) ON CONFLICT DO NOTHING`, version, evidenceJSON)
	}
	if err == nil {
		command, policyErr := tx.Exec(ctx, `INSERT INTO pricing_quote_policies(version,policy) SELECT $1,p.policy FROM pricing_drafts d JOIN pricing_quote_policies p ON p.version=d.parent_version WHERE d.version=$1`, version)
		err = policyErr
		if err == nil && command.RowsAffected() != 1 {
			err = errors.New("quote policy missing from pricing ancestry")
		}
	}
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE pricing_drafts SET status='approved',reviewed_by=$2,reviewed_at=now() WHERE version=$1`, version, actor)
	}
	if err == nil {
		auditAction := "version_approved"
		if action == "schedule" {
			auditAction = "version_scheduled"
		}
		err = insertAudit(ctx, tx, actor, auditAction, version, class, in.Reason, requestID(r), `{"approved":true}`)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		slog.ErrorContext(ctx, "pricing approval persistence failed", "request_id", requestID(r), "error", err)
		adminError(w, 409, "could not approve pricing version")
		return
	}
	a.emit(ctx, actor, version, "pricing_version_approved", class)
	statusText := "approved"
	var future bool
	if err := a.pool.QueryRow(ctx, "SELECT $1>now()", parsed.EffectiveFrom).Scan(&future); err != nil {
		future = false
	}
	if future {
		statusText = "scheduled"
	}
	adminJSON(w, 200, map[string]string{"version": version, "status": statusText})
}

func (a *Admin) cancel(w http.ResponseWriter, r *http.Request, actor, version, reason string) {
	ctx := r.Context()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		adminError(w, 503, "pricing store unavailable")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var effective time.Time
	err = tx.QueryRow(ctx, "SELECT effective_from FROM pricing_versions WHERE version=$1 AND approved FOR UPDATE", version).Scan(&effective)
	if errors.Is(err, pgx.ErrNoRows) {
		adminError(w, 404, "approved pricing version not found")
		return
	}
	if err != nil {
		adminError(w, 503, "pricing version unavailable")
		return
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT now()").Scan(&now); err != nil {
		adminError(w, 503, "database clock unavailable")
		return
	}
	if !effective.After(now) {
		a.auditFailure(ctx, actor, "cancellation_denied", version, reason, "{}", requestID(r))
		adminError(w, 409, "only a future scheduled version can be cancelled")
		return
	}
	_, err = tx.Exec(ctx, "INSERT INTO pricing_version_cancellations(version,cancelled_by,reason) VALUES($1,$2,$3)", version, actorHash(actor), reason)
	if err == nil {
		err = insertAudit(ctx, tx, actor, "version_cancelled", version, "", reason, requestID(r), `{"cancelled":true}`)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		adminError(w, 409, "could not cancel scheduled pricing version")
		return
	}
	adminJSON(w, 200, map[string]string{"version": version, "status": "cancelled"})
}

func (a *Admin) rollback(w http.ResponseWriter, r *http.Request, actor, source string) {
	var in rollbackInput
	if !decodeAdmin(w, r, &in) {
		a.auditFailure(r.Context(), actor, "rollback_denied", source, "invalid request", "{}", requestID(r))
		return
	}
	if err := validateReason(in.Reason); err != nil || in.Version == "" || in.EffectiveFrom.IsZero() {
		a.auditFailure(r.Context(), actor, "rollback_denied", source, in.Reason, "{}", requestID(r))
		adminError(w, 400, "version, effective_from and reason are required")
		return
	}
	ctx := r.Context()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		adminError(w, 503, "pricing store unavailable")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var historical []byte
	if err = tx.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version=$1 AND approved", source).Scan(&historical); err != nil {
		adminError(w, 404, "rollback source not found")
		return
	}
	var parent string
	var active []byte
	if err = tx.QueryRow(ctx, `SELECT version,config FROM pricing_versions WHERE approved AND effective_from<=now() AND NOT EXISTS (SELECT 1 FROM pricing_version_cancellations c WHERE c.version=pricing_versions.version) ORDER BY effective_from DESC LIMIT 1 FOR SHARE`).Scan(&parent, &active); err != nil {
		adminError(w, 503, "active pricing unavailable")
		return
	}
	var duplicate bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pricing_versions WHERE version=$1) OR EXISTS(SELECT 1 FROM pricing_drafts WHERE version=$1)", in.Version).Scan(&duplicate); err != nil || duplicate {
		adminError(w, 409, "pricing version already exists")
		return
	}
	var doc map[string]json.RawMessage
	if err = json.Unmarshal(historical, &doc); err != nil {
		adminError(w, 422, "rollback source invalid")
		return
	}
	doc["version"], _ = json.Marshal(in.Version)
	doc["effective_from"], _ = json.Marshal(in.EffectiveFrom.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano))
	content, _ := json.Marshal(doc)
	parsed, err := Parse(content)
	if err != nil {
		adminError(w, 422, "rollback config invalid")
		return
	}
	content, parsed, err = normalizeEffectiveFrom(content)
	if err != nil {
		adminError(w, 422, "rollback config invalid")
		return
	}
	if err = ValidateEconomicsWithContext(ctx, content, a.verifier, in.OverrideReason); err != nil {
		adminError(w, 422, err.Error())
		return
	}
	adverse, err := classifyAdverse(active, content)
	if err != nil {
		adminError(w, 422, "unable to classify rollback")
		return
	}
	class := "non_adverse"
	if adverse {
		class = "adverse"
	}
	summary, err := diffSummary(active, content)
	if err != nil {
		adminError(w, 422, "rollback diff unavailable")
		return
	}
	if _, err = tx.Exec(ctx, "INSERT INTO pricing_drafts(version,parent_version,config,created_by,change_class,rollback_source_version) VALUES($1,$2,$3::jsonb,$4,$5,$6)", parsed.Version, parent, content, actor, class, source); err == nil {
		err = insertAudit(ctx, tx, actor, "version_rolled_back", parsed.Version, class, in.Reason, requestID(r), summary)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		adminError(w, 409, "could not create rollback draft")
		return
	}
	a.emit(ctx, actor, parsed.Version, "pricing_version_rolled_back", class)
	adminJSON(w, 201, map[string]string{"version": parsed.Version, "parent_version": parent, "rollback_source": source, "status": "draft", "change_class": class})
}

func (a *Admin) list(w http.ResponseWriter, r *http.Request, actor string) {
	reason := r.URL.Query().Get("reason")
	if err := validateReason(reason); err != nil {
		a.auditFailure(r.Context(), actor, "validation_failed", "", reason, "{}", requestID(r))
		adminError(w, 400, "a reason query parameter is required")
		return
	}
	rows, err := a.pool.Query(r.Context(), `SELECT version,status,coalesce(change_class,''),coalesce(created_by,''),created_at FROM pricing_drafts
UNION ALL
SELECT p.version,CASE WHEN p.effective_from<=now() THEN 'active' ELSE 'approved' END,coalesce(d.change_class,''),coalesce(d.created_by,''),p.created_at
FROM pricing_versions p LEFT JOIN pricing_drafts d ON d.version=p.version WHERE d.version IS NULL
ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		adminError(w, 503, "pricing list unavailable")
		return
	}
	defer rows.Close()
	type row struct {
		Version     string    `json:"version"`
		Status      string    `json:"status"`
		ChangeClass string    `json:"change_class"`
		Author      string    `json:"author_id"`
		CreatedAt   time.Time `json:"created_at"`
	}
	items := []row{}
	for rows.Next() {
		var v row
		if err = rows.Scan(&v.Version, &v.Status, &v.ChangeClass, &v.Author, &v.CreatedAt); err != nil {
			adminError(w, 503, "pricing list unavailable")
			return
		}
		items = append(items, v)
	}
	if rows.Err() != nil {
		adminError(w, 503, "pricing list unavailable")
		return
	}
	a.auditFailure(r.Context(), actor, "versions_listed", "", reason, "{}", requestID(r))
	adminJSON(w, 200, map[string]any{"drafts": items})
}

func (a *Admin) show(w http.ResponseWriter, r *http.Request, actor, version string) {
	reason := r.URL.Query().Get("reason")
	if err := validateReason(reason); err != nil {
		a.auditFailure(r.Context(), actor, "validation_failed", version, reason, "{}", requestID(r))
		adminError(w, 400, "a reason query parameter is required")
		return
	}
	var cfg []byte
	var status, class, parent, author string
	var created time.Time
	err := a.pool.QueryRow(r.Context(), `SELECT config,status,change_class,parent_version,created_by,created_at FROM pricing_drafts WHERE version=$1`, version).Scan(&cfg, &status, &class, &parent, &author, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		err = a.pool.QueryRow(r.Context(), `SELECT config,CASE WHEN effective_from<=now() THEN 'active' ELSE 'approved' END,created_at FROM pricing_versions WHERE version=$1`, version).Scan(&cfg, &status, &created)
		if errors.Is(err, pgx.ErrNoRows) {
			a.auditFailure(r.Context(), actor, "version_show_denied", version, reason, "{}", requestID(r))
			adminError(w, 404, "pricing version not found")
			return
		}
		if err != nil {
			adminError(w, 503, "pricing version unavailable")
			return
		}
		class = ""
		parent = ""
		author = ""
		a.auditFailure(r.Context(), actor, "version_shown", version, reason, "{}", requestID(r))
		adminJSON(w, 200, map[string]any{"version": version, "config": json.RawMessage(cfg), "status": status, "change_class": class, "parent_version": parent, "author_id": author, "created_at": created})
		return
	}
	if err != nil {
		adminError(w, 503, "pricing draft unavailable")
		return
	}
	a.auditFailure(r.Context(), actor, "version_shown", version, reason, "{}", requestID(r))
	adminJSON(w, 200, map[string]any{"version": version, "config": json.RawMessage(cfg), "status": status, "change_class": class, "parent_version": parent, "author_id": author, "created_at": created})
}

func changedPaths(before, after []byte) ([]string, error) {
	var left, right map[string]any
	if err := json.Unmarshal(before, &left); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(after, &right); err != nil {
		return nil, err
	}
	paths := []string{}
	collectChanged(left, right, "", &paths)
	sort.Strings(paths)
	return paths, nil
}

func diffSummary(before, after []byte) (string, error) {
	paths, err := changedPaths(before, after)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(map[string][]string{"changed_fields": paths})
	return string(raw), err
}

func collectChanged(left, right map[string]any, prefix string, paths *[]string) {
	for key, old := range left {
		if prefix == "" && (key == "version" || key == "effective_from" || key == "development_mappings") {
			continue
		}
		if key == "stripe_price_id" || key == "store_product_ids" {
			continue
		}
		name := key
		if prefix != "" {
			name = prefix + "." + key
		}
		newValue, ok := right[key]
		if !ok {
			*paths = append(*paths, name)
			continue
		}
		collectChangedValue(old, newValue, name, paths)
	}
	for key := range right {
		if _, ok := left[key]; !ok && key != "version" && key != "effective_from" && key != "development_mappings" {
			*paths = append(*paths, key)
		}
	}
}

func collectChangedValue(old, next any, path string, paths *[]string) {
	if jsonEqual(old, next) {
		return
	}
	oldMap, oldMapOK := old.(map[string]any)
	newMap, newMapOK := next.(map[string]any)
	if oldMapOK && newMapOK {
		collectChanged(oldMap, newMap, path, paths)
		return
	}
	oldList, oldListOK := old.([]any)
	newList, newListOK := next.([]any)
	if oldListOK && newListOK && (strings.Contains(path, "packs") || strings.Contains(path, "plans")) {
		left, right := indexList(oldList), indexList(newList)
		for key, value := range left {
			other, ok := right[key]
			if !ok {
				*paths = append(*paths, path+"."+key)
			} else {
				collectChangedValue(value, other, path+"."+key, paths)
			}
		}
		for key := range right {
			if _, ok := left[key]; !ok {
				*paths = append(*paths, path+"."+key)
			}
		}
		return
	}
	*paths = append(*paths, path)
}

func insertAudit(ctx context.Context, tx pgx.Tx, actor, action, version, class, reason, request, summary string) error {
	if err := validateReason(reasonOrSystem(reason)); err != nil {
		return err
	}
	if strings.TrimSpace(class) == "" {
		class = ""
	}
	_, err := tx.Exec(ctx, `INSERT INTO pricing_audit(actor_id,action,version,change_class,diff_summary,reason,request_id)
VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),$5::jsonb,$6,$7)`, actorHash(actor), action, version, class, summary, strings.TrimSpace(reasonOrSystem(reason)), request)
	return err
}
func reasonOrSystem(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return "system denial"
	}
	return reason
}

func (a *Admin) auditFailure(ctx context.Context, actor, action, version, reason, summary string, requestIDs ...string) {
	if strings.TrimSpace(reason) == "" {
		reason = "pricing operation"
	}
	request := "system"
	if len(requestIDs) > 0 && strings.TrimSpace(requestIDs[0]) != "" {
		request = requestIDs[0]
	}
	_, _ = a.pool.Exec(ctx, `INSERT INTO pricing_audit(actor_id,action,version,change_class,diff_summary,reason,request_id) VALUES($1,$2,NULLIF($3,''),NULLIF($4,'')::text,$5::jsonb,$6,$7)`, actorHash(actor), action, version, "", summary, reason, request)
}

func (a *Admin) emit(ctx context.Context, actor, version, event, class string) {
	if a.events == nil {
		return
	}
	var optedOut bool
	if err := a.pool.QueryRow(ctx, `SELECT analytics_opt_out FROM learner_profiles WHERE learner_id=$1::uuid`, actor).Scan(&optedOut); err != nil || optedOut {
		return
	}
	_ = a.events.Capture(actor, event, map[string]any{"pricing_version": version, "change_class": class, "actor_id": actor, "platform": "web", "app_version": "development", "source": "api"})
}

// BootstrapAdmins grants pricing_admin only to pre-existing operator-selected accounts.
func BootstrapAdmins(ctx context.Context, pool *pgxpool.Pool, ids string) error {
	for _, raw := range strings.Split(ids, ",") {
		id := strings.ToLower(strings.TrimSpace(raw))
		if id == "" {
			continue
		}
		if !pricingUUID.MatchString(id) {
			return fmt.Errorf("invalid pricing admin account UUID")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid) ON CONFLICT(account_id) DO NOTHING`, id); err != nil {
			return err
		}
	}
	return nil
}

// classifyAdverse compares every learner-facing pricing field and fails closed
// for changed fields whose direction is not defined by the business rules.
func classifyAdverse(previous, next []byte) (bool, error) {
	var before, after map[string]any
	if err := json.Unmarshal(previous, &before); err != nil {
		return false, err
	}
	if err := json.Unmarshal(next, &after); err != nil {
		return false, err
	}
	return adverseObject(before, after, ""), nil
}

func adverseObject(before, after map[string]any, path string) bool {
	for key, oldValue := range before {
		newValue, ok := after[key]
		if !ok {
			if path == "" && (key == "version" || key == "effective_from" || key == "development_mappings") {
				continue
			}
			if key == "stripe_price_id" || key == "store_product_ids" {
				continue
			}
			return true
		}
		full := key
		if path != "" {
			full = path + "." + key
		}
		if path == "" && (key == "version" || key == "effective_from" || key == "development_mappings") {
			continue
		}
		if key == "stripe_price_id" || key == "store_product_ids" {
			continue
		}
		if !adverseValue(oldValue, newValue, full) {
			continue
		}
		return true
	}
	for key := range after {
		if _, ok := before[key]; !ok && key != "version" && key != "effective_from" && key != "development_mappings" {
			if key == "stripe_price_id" || key == "store_product_ids" {
				continue
			}
			return true
		}
	}
	return false
}

func adverseValue(old, next any, path string) bool {
	if oldMap, ok := old.(map[string]any); ok {
		newMap, ok := next.(map[string]any)
		if !ok {
			return true
		}
		return adverseObject(oldMap, newMap, path)
	}
	if oldList, ok := old.([]any); ok {
		newList, ok := next.([]any)
		if !ok {
			return true
		}
		if strings.Contains(path, "entitlements") || strings.Contains(path, "benefits") || strings.Contains(path, "eligible_actions") {
			for _, v := range oldList {
				if !containsJSON(newList, v) {
					return true
				}
			}
			return false
		}
		if strings.Contains(path, "packs") || strings.Contains(path, "plans") {
			bySKU := indexList(oldList)
			after := indexList(newList)
			for sku, oldItem := range bySKU {
				newItem, ok := after[sku]
				if !ok {
					return true
				}
				if adverseValue(oldItem, newItem, path+"."+sku) {
					return true
				}
			}
			for sku := range after {
				if _, ok := bySKU[sku]; !ok {
					return true
				}
			}
			return false
		}
		return !jsonEqual(old, next)
	}
	oldNumber, oldOK := number(old)
	newNumber, newOK := number(next)
	if oldOK && newOK {
		if oldNumber == newNumber {
			return false
		}
		if strings.Contains(path, "actions.") && strings.HasSuffix(path, ".credits") {
			return newNumber > oldNumber
		}
		lowerIsAdverse := strings.Contains(path, "daily_free") || ((strings.Contains(path, "packs") || strings.Contains(path, "plans")) && strings.Contains(path, "credits")) || strings.Contains(path, "concurr") || strings.Contains(path, "limit") || strings.Contains(path, "max_") || strings.Contains(path, "eligible")
		if strings.Contains(path, "amount_minor") || strings.Contains(path, "price") {
			return newNumber > oldNumber
		}
		if lowerIsAdverse {
			return newNumber < oldNumber
		}
		return true
	}
	return !jsonEqual(old, next)
}

func indexList(items []any) map[string]any {
	out := map[string]any{}
	for i, item := range items {
		if m, ok := item.(map[string]any); ok {
			key, _ := m["sku"].(string)
			if key == "" {
				key, _ = m["id"].(string)
			}
			if key == "" {
				key = fmt.Sprint(i)
			}
			out[key] = m
		}
	}
	return out
}
func containsJSON(items []any, value any) bool {
	for _, item := range items {
		if jsonEqual(item, value) {
			return true
		}
	}
	return false
}
func jsonEqual(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func validateMargins(raw []byte) error {
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}
	limits, _ := config["limits"].(map[string]any)
	cost, _ := number(limits["protected_cost_ceiling_usd_per_credit"])
	if cost == 0 {
		cost, _ = number(limits["protected_cost_ceiling_usd_per_credit"])
	}
	if cost <= 0 {
		return errors.New("missing protected cost ceiling")
	}
	packs, _ := config["packs"].([]any)
	for _, item := range packs {
		m, _ := item.(map[string]any)
		sku, _ := m["sku"].(string)
		price, _ := number(m["amount_minor"])
		if price == 0 {
			price, _ = number(m["price_minor"])
		}
		credits, _ := number(m["credits"])
		if margin(price/100, credits*cost) < 70 {
			return fmt.Errorf("SKU %s is below 70%% margin", sku)
		}
	}
	plans, _ := config["plans"].([]any)
	for _, item := range plans {
		m, _ := item.(map[string]any)
		credits, _ := number(m["monthly_credits"])
		for _, key := range []string{"monthly", "yearly"} {
			offer, _ := m[key].(map[string]any)
			sku, _ := offer["sku"].(string)
			price, _ := number(offer["amount_minor"])
			factor := 1.0
			if key == "yearly" {
				factor = 12
			}
			if margin(price/100, credits*factor*cost) < 70 {
				return fmt.Errorf("SKU %s is below 70%% margin", sku)
			}
		}
	}
	return nil
}
func margin(price, cost float64) float64 {
	if price <= 0 {
		return -math.MaxFloat64
	}
	return 100 * (price - cost) / price
}

// OperatorPricingAdminIDs exposes the deployment bootstrap setting to startup.
func OperatorPricingAdminIDs() string { return os.Getenv("PRICING_ADMIN_IDS") }
