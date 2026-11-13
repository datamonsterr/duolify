// Package pricing validates and serves the immutable, database-owned catalogue.
package pricing

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schema.json
var schemaJSON []byte

// Action is a standard-envelope service minimum, not a client-side quote.
type Action struct {
	Credits int `json:"credits"`
}

// Offer holds the money value for one billing interval.
type Offer struct {
	SKU             string            `json:"sku"`
	Currency        string            `json:"currency"`
	AmountMinor     int               `json:"amount_minor"`
	StripePriceID   any               `json:"stripe_price_id,omitempty"`
	StoreProductIDs map[string]string `json:"store_product_ids,omitempty"`
}

// Pack is a non-expiring one-time credit offer.
type Pack struct {
	Offer
	Name            string            `json:"name"`
	Credits         int               `json:"credits"`
	StripePriceID   any               `json:"stripe_price_id,omitempty"`
	StoreProductIDs map[string]string `json:"store_product_ids,omitempty"`
}

// Plan benefits and limits are config-owned, including annual monthly issuance.
type Plan struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Monthly              Offer    `json:"monthly"`
	Yearly               Offer    `json:"yearly"`
	MonthlyCredits       int      `json:"monthly_credits"`
	ConcurrentGeneration int      `json:"concurrent_generation"`
	Benefits             []string `json:"benefits"`
}

// Config is the validated immutable pricing document. Public excludes mappings.
type Config struct {
	Version       string            `json:"version"`
	EffectiveFrom time.Time         `json:"effective_from"`
	Currency      string            `json:"currency"`
	Actions       map[string]Action `json:"actions"`
	Narration     struct {
		Credits    int    `json:"credits"`
		Characters int    `json:"characters"`
		Rounding   string `json:"rounding"`
	} `json:"narration"`
	DailyFree struct {
		Credits         int      `json:"credits"`
		EligibleActions []string `json:"eligible_actions"`
		Expires         string   `json:"expires"`
		SpendFirst      bool     `json:"spend_first"`
	} `json:"daily_free"`
	Packs               []Pack                       `json:"packs"`
	Plans               []Plan                       `json:"plans"`
	PaidCredits         json.RawMessage              `json:"paid_credits"`
	Limits              json.RawMessage              `json:"limits"`
	ProcessingFees      *ProviderRateConfig          `json:"processing_fees,omitempty"`
	DevelopmentMappings map[string]string            `json:"development_mappings,omitempty"`
	StripeMappings      map[string]any               `json:"stripe_mappings,omitempty"`
	StoreMappings       map[string]map[string]string `json:"store_mappings,omitempty"`
	DesignChatDecision  string                       `json:"design_chat_decision,omitempty"`
}

// Public strips provider mappings. No sales are activated by this read-only ticket.
func (c Config) Public() PublicConfig {
	c.DevelopmentMappings = nil
	c.ProcessingFees = nil
	c.StripeMappings = nil
	c.StoreMappings = nil
	return PublicConfig{Config: c, SalesEnabled: false}
}

// PublicConfig is the versioned GET /v1/pricing DTO.
type PublicConfig struct {
	Config
	SalesEnabled  bool    `json:"sales_enabled"`
	NextScheduled *Config `json:"next_scheduled"`
}

// ProviderMappings is deliberately unusable in any environment except development.
func (c Config) ProviderMappings(environment string) map[string]string {
	if environment != "development" {
		return nil
	}
	result := make(map[string]string, len(c.DevelopmentMappings))
	for k, v := range c.DevelopmentMappings {
		result[k] = v
	}
	return result
}

// Parse enforces JSON Schema followed by relationships that JSON Schema cannot express.
func Parse(raw []byte) (Config, error) {
	var zero Config
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return zero, fmt.Errorf("pricing JSON: %w", err)
	}
	schema, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		return zero, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource("urn:duolify:pricing", schema); err != nil {
		return zero, err
	}
	compiled, err := compiler.Compile("urn:duolify:pricing")
	if err != nil {
		return zero, err
	}
	if err := compiled.Validate(document); err != nil {
		return zero, fmt.Errorf("pricing schema: %w", err)
	}
	var c Config
	canonical, err := json.Marshal(document)
	if err != nil {
		return zero, err
	}
	if err := json.Unmarshal(canonical, &c); err != nil {
		return zero, err
	}
	if c.Narration.Credits != c.Actions["narration"].Credits {
		return zero, errors.New("narration.credits must match actions.narration.credits")
	}
	for _, key := range c.DailyFree.EligibleActions {
		if _, ok := c.Actions[key]; !ok {
			return zero, fmt.Errorf("daily_free.eligible_actions: unknown action %s", key)
		}
	}
	seen := map[string]bool{}
	check := func(o Offer) error {
		if seen[o.SKU] {
			return fmt.Errorf("duplicate SKU: %s", o.SKU)
		}
		seen[o.SKU] = true
		if o.Currency != c.Currency {
			return fmt.Errorf("%s.currency differs from catalogue currency", o.SKU)
		}
		return nil
	}
	for _, p := range c.Packs {
		if err := check(p.Offer); err != nil {
			return zero, err
		}
	}
	plans := map[string]bool{}
	for _, p := range c.Plans {
		if plans[p.ID] {
			return zero, fmt.Errorf("duplicate plan id: %s", p.ID)
		}
		plans[p.ID] = true
		for _, o := range []Offer{p.Monthly, p.Yearly} {
			if err := check(o); err != nil {
				return zero, err
			}
		}
	}
	if len(c.DevelopmentMappings) != len(seen) {
		return zero, errors.New("development_mappings must cover each SKU exactly once")
	}
	for sku := range seen {
		if c.DevelopmentMappings[sku] != "synthetic:"+sku {
			return zero, fmt.Errorf("development_mappings.%s must be explicitly synthetic", sku)
		}
	}
	return c, nil
}

// Queries is implemented by sqlc and permits deterministic failure tests.
type Queries interface {
	ActivePricing(context.Context) (db.ActivePricingRow, error)
	NextPricing(context.Context) (db.NextPricingRow, error)
}

// Catalogue contains only validated state and canonical bytes for an exact ETag.
type Catalogue struct {
	config Config
	body   []byte
	etag   string
}

// IsDevOrTest reports whether env is explicit development or test.
// Any other value (including empty or "production") is treated as production (fail closed).
func IsDevOrTest(env string) bool {
	return env == "development" || env == "test"
}

// IsDesignChatAction reports whether the action is one of the design chat actions.
func IsDesignChatAction(action string) bool {
	return action == "setup_chat" || action == "course_brief_chat" || action == "outline_chat_change"
}

// HasDesignChatKeys reports whether the config contains any design chat actions.
func HasDesignChatKeys(c Config) bool {
	if c.Actions == nil {
		return false
	}
	_, hasSetup := c.Actions["setup_chat"]
	_, hasBrief := c.Actions["course_brief_chat"]
	_, hasOutline := c.Actions["outline_chat_change"]
	return hasSetup || hasBrief || hasOutline
}

// CheckDesignChatDecisionRecorded validates only the recorded decision value.
// Use it on read paths where activation already enforced the owner identity.
func CheckDesignChatDecisionRecorded(c Config, env string) error {
	if !HasDesignChatKeys(c) {
		return nil
	}
	if !IsDevOrTest(env) {
		if c.DesignChatDecision != "owner_approved" {
			return errors.New("design chat pricing requires owner approval in production")
		}
		return nil
	}
	if c.DesignChatDecision != "interim_adr_0012" && c.DesignChatDecision != "owner_approved" {
		return errors.New("design chat pricing requires recorded decision (interim_adr_0012 or owner_approved)")
	}
	return nil
}

// CheckDesignChatDecision is the single enforcement point for the design chat
// decision gate. In production it fails closed: the decision must be
// owner_approved, PRICING_OWNER_ID must be set, and approver (the reviewer of
// the version) must be non-empty and equal to it.
func CheckDesignChatDecision(c Config, env string, approver string, ownerID string) error {
	if err := CheckDesignChatDecisionRecorded(c, env); err != nil {
		return err
	}
	if HasDesignChatKeys(c) && !IsDevOrTest(env) {
		if ownerID == "" || approver == "" || !strings.EqualFold(approver, ownerID) {
			return errors.New("design chat pricing requires owner approval in production")
		}
	}
	return nil
}

// RuntimeCatalogue resolves approved versions using Postgres time on every
// read. Postgres remains available when the optional Redis cache is down; a
// rejected reload leaves the last validated response available to clients.
type RuntimeCatalogue struct {
	mu       sync.RWMutex
	queries  Queries
	pool     *pgxpool.Pool
	events   analytics.Events
	last     *Catalogue
	baseline time.Time
	env      string
	ownerID  string
	// gateSkipped dedupes refusal logs for versions blocked by the design chat gate.
	gateSkipped map[string]bool
}

// NewRuntimeCatalogue creates the database-time pricing resolver with a validated startup fallback.
func NewRuntimeCatalogue(initial *Catalogue, q Queries, pool *pgxpool.Pool, events analytics.Events) *RuntimeCatalogue {
	return &RuntimeCatalogue{
		queries:  q,
		pool:     pool,
		events:   events,
		last:     initial,
		baseline: initial.config.EffectiveFrom,
		env:      os.Getenv("DUOLIFY_ENV"),
		ownerID:  os.Getenv("PRICING_OWNER_ID"),
	}
}

// SetEnvironment configures the deployment environment for runtime activation checks.
func (c *RuntimeCatalogue) SetEnvironment(env string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.env = env
}

// SetOwnerID configures the pricing owner ID for runtime checks.
func (c *RuntimeCatalogue) SetOwnerID(ownerID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ownerID = ownerID
}

// Config returns a copy of the current active config.
func (c *RuntimeCatalogue) Config() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.last == nil {
		return Config{}
	}
	return c.last.Config()
}

// HasAction reports whether the active catalogue defines the given action and passes the gate.
func (c *RuntimeCatalogue) HasAction(name string) bool {
	c.mu.RLock()
	last := c.last
	env := c.env
	c.mu.RUnlock()
	if last == nil {
		return false
	}
	if !last.HasAction(name) {
		return false
	}
	if IsDesignChatAction(name) {
		return CheckDesignChatDecisionRecorded(last.config, env) == nil
	}
	return true
}

func (c *RuntimeCatalogue) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	loaded, err := Load(r.Context(), c.queries)
	if err != nil {
		slog.ErrorContext(r.Context(), "pricing runtime reload failed; serving last known good catalogue", "error", err)
		c.mu.RLock()
		last := c.last
		c.mu.RUnlock()
		if last == nil {
			http.Error(w, "pricing unavailable", http.StatusServiceUnavailable)
			return
		}
		last.ServeHTTP(w, r)
		return
	}
	c.mu.Lock()
	previous := c.last
	if previous != nil && previous.config.Version != loaded.config.Version && c.pool != nil {
		var activated bool
		if err := c.pool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM pricing_activation_events WHERE version=$1)", loaded.config.Version).Scan(&activated); err != nil || !activated {
			c.mu.Unlock()
			previous.ServeHTTP(w, r)
			return
		}
	}
	c.last = loaded
	c.mu.Unlock()
	loaded.ServeHTTP(w, r)
}

// RunActivator reconciles due versions on startup and every 200ms using the
// database clock. Multiple API replicas share the once-only marker transaction.
func (c *RuntimeCatalogue) RunActivator(ctx context.Context, admin *Admin) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := c.ActivateDue(ctx, admin); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "pricing activation refused", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ActivateDue verifies and records due approved versions independently of public reads.
func (c *RuntimeCatalogue) ActivateDue(ctx context.Context, admin *Admin) error {
	if c.pool == nil || admin == nil {
		return errors.New("pricing activation requires an external mapping verifier")
	}
	// Baseline seeded versions have no admin approval. Reconcile every later due
	// version and any approval missed during downtime, even if now superseded.
	rows, err := c.pool.Query(ctx, `SELECT p.version,p.config,coalesce((SELECT a.override_reason FROM pricing_audit a WHERE a.version=p.version AND a.action IN ('version_approved','version_scheduled') ORDER BY a.created_at DESC LIMIT 1),'')
 FROM pricing_versions p WHERE p.approved AND p.effective_from<=clock_timestamp()
 AND (p.effective_from>$1 OR EXISTS(SELECT 1 FROM pricing_drafts d WHERE d.version=p.version AND d.status='approved'))
 AND NOT EXISTS(SELECT 1 FROM pricing_version_cancellations x WHERE x.version=p.version)
 AND NOT EXISTS(SELECT 1 FROM pricing_activation_events e WHERE e.version=p.version)
 ORDER BY p.effective_from`, c.baseline)
	if err != nil {
		return err
	}
	type dueVersion struct {
		version  string
		raw      []byte
		override string
	}
	var due []dueVersion
	for rows.Next() {
		var v dueVersion
		if err = rows.Scan(&v.version, &v.raw, &v.override); err != nil {
			rows.Close()
			return err
		}
		due = append(due, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(due) > 0 && admin.verifier == nil {
		return errors.New("pricing activation requires an external mapping verifier")
	}
	for _, v := range due {
		parsed, err := Parse(v.raw)
		if err != nil {
			return err
		}
		if HasDesignChatKeys(parsed) {
			var reviewer string
			_ = c.pool.QueryRow(ctx, "SELECT coalesce((SELECT reviewed_by FROM pricing_drafts WHERE version=$1),'')", v.version).Scan(&reviewer)
			if gateErr := CheckDesignChatDecision(parsed, c.env, reviewer, admin.owner); gateErr != nil {
				// Skip only this version; the last good catalogue stays public and
				// unrelated due versions still activate.
				c.mu.Lock()
				seen := c.gateSkipped[v.version]
				if c.gateSkipped == nil {
					c.gateSkipped = map[string]bool{}
				}
				c.gateSkipped[v.version] = true
				c.mu.Unlock()
				if !seen {
					slog.WarnContext(ctx, "pricing activation refused", "version", v.version, "error", gateErr)
				}
				continue
			}
		}
		if _, err = checkEconomics(ctx, v.raw, admin.verifier, v.override, admin.policy, false); err != nil {
			return fmt.Errorf("version %s: %w", v.version, err)
		}
		if err = c.recordActivation(ctx, v.version); err != nil {
			return err
		}
		if loaded, err := Load(ctx, c.queries); err == nil {
			c.mu.Lock()
			c.last = loaded
			c.mu.Unlock()
		}
	}
	return nil
}

func (c *RuntimeCatalogue) recordActivation(ctx context.Context, version string) error {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var inserted string
	err = tx.QueryRow(ctx, `INSERT INTO pricing_activation_events(version,activated_at)
 SELECT p.version,p.effective_from FROM pricing_versions p
 WHERE p.version=$1 AND p.approved AND p.effective_from<=clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM pricing_version_cancellations x WHERE x.version=p.version)
 ON CONFLICT DO NOTHING RETURNING version`, version).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	class := "non_adverse"
	if err = tx.QueryRow(ctx, "SELECT coalesce((SELECT change_class FROM pricing_drafts WHERE version=$1),'non_adverse')", version).Scan(&class); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO pricing_audit(actor_id,action,version,change_class,reason,request_id,created_at)
 SELECT $1,'version_activated',version,$3,'scheduled activation',$4,effective_from FROM pricing_versions WHERE version=$2`, actorHash(systemActor), version, class, "activation-"+version); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if c.events != nil {
		properties := map[string]any{"pricing_version": version, "change_class": class, "actor_id": systemActor, "platform": "web", "app_version": "development", "source": "api"}
		_ = c.events.Capture(systemActor, "pricing_version_activated", properties)
		_ = c.events.Capture(systemActor, "price_table_activated", properties)
	}
	return nil
}

// Load is called before listening; absent or corrupt active data is a boot error.
func Load(ctx context.Context, q Queries) (*Catalogue, error) {
	row, err := q.ActivePricing(ctx)
	if err != nil {
		return nil, fmt.Errorf("load active pricing: %w", err)
	}
	c, err := Parse(row.Config)
	if err != nil {
		return nil, err
	}
	if c.Version != row.Version || !c.EffectiveFrom.Equal(row.EffectiveFrom.Time) {
		return nil, errors.New("active pricing metadata mismatch")
	}
	dto := c.Public()
	next, err := q.NextPricing(ctx)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load next pricing: %w", err)
	}
	if err == nil {
		n, parseErr := Parse(next.Config)
		if parseErr != nil {
			return nil, parseErr
		}
		if n.Version != next.Version || !n.EffectiveFrom.Equal(next.EffectiveFrom.Time) {
			return nil, errors.New("next pricing metadata mismatch")
		}
		n.DevelopmentMappings = nil
		dto.NextScheduled = &n
	}
	body, err := json.Marshal(dto)
	if err != nil {
		return nil, err
	}
	return &Catalogue{config: c, body: body, etag: fmt.Sprintf(`"%x"`, sha256.Sum256(body))}, nil
}

// FromConfig builds a Catalogue from an in-memory validated Config.
func FromConfig(c Config) *Catalogue {
	dto := c.Public()
	body, _ := json.Marshal(dto)
	return &Catalogue{config: c, body: body, etag: fmt.Sprintf(`"%x"`, sha256.Sum256(body))}
}

// LoadDevelopment loads the unapproved development pricing version if present,
// or resolves through the active version once an activated version carries the keys.
func LoadDevelopment(ctx context.Context, pool *pgxpool.Pool) (*Catalogue, error) {
	var configJSON []byte
	// The currently active version is authoritative: it carries the keys once
	// activated, and after a rollback (active version without keys) an older
	// version with keys must not be resurrected.
	if row, err := db.New(pool).ActivePricing(ctx); err == nil {
		if c, parseErr := Parse(row.Config); parseErr == nil {
			if HasDesignChatKeys(c) {
				return FromConfig(c), nil
			}
			var activatedEver bool
			_ = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pricing_activation_events e JOIN pricing_versions v ON v.version=e.version WHERE v.config->'actions' ?| array['setup_chat','course_brief_chat','outline_chat_change'])").Scan(&activatedEver)
			if activatedEver {
				return FromConfig(c), nil
			}
		}
	}
	err := pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version IN ('usd-dev-unapproved','usd-dev-course-brief-unapproved') ORDER BY (version='usd-dev-course-brief-unapproved') DESC LIMIT 1").Scan(&configJSON)
	if err != nil {
		return nil, err
	}
	c, err := Parse(configJSON)
	if err != nil {
		return nil, err
	}
	return FromConfig(c), nil
}

// HasAction reports whether the catalogue defines the given action.
func (c *Catalogue) HasAction(name string) bool {
	if c == nil {
		return false
	}
	_, ok := c.config.Actions[name]
	return ok
}

// Config returns a copy so consumers cannot mutate the validated catalogue.
func (c *Catalogue) Config() Config {
	raw, _ := json.Marshal(c.config)
	var result Config
	_ = json.Unmarshal(raw, &result)
	return result
}

// ServeHTTP returns a read-only catalogue with bounded freshness and conditional GET.
func (c *Catalogue) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("ETag", c.etag)
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Header().Set("Content-Type", "application/json")
	for _, tag := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" || strings.TrimPrefix(tag, "W/") == c.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	_, _ = w.Write(c.body)
}
