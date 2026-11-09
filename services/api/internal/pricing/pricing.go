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
	SKU         string `json:"sku"`
	Currency    string `json:"currency"`
	AmountMinor int    `json:"amount_minor"`
}

// Pack is a non-expiring one-time credit offer.
type Pack struct {
	Offer
	Name    string `json:"name"`
	Credits int    `json:"credits"`
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
	Packs               []Pack            `json:"packs"`
	Plans               []Plan            `json:"plans"`
	PaidCredits         json.RawMessage   `json:"paid_credits"`
	Limits              json.RawMessage   `json:"limits"`
	DevelopmentMappings map[string]string `json:"development_mappings,omitempty"`
}

// Public strips provider mappings. No sales are activated by this read-only ticket.
func (c Config) Public() PublicConfig {
	c.DevelopmentMappings = nil
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

// RuntimeCatalogue resolves approved versions using Postgres time on every
// read. Postgres remains available when the optional Redis cache is down; a
// rejected reload leaves the last validated response available to clients.
type RuntimeCatalogue struct {
	mu      sync.RWMutex
	queries Queries
	pool    *pgxpool.Pool
	events  analytics.Events
	last    *Catalogue
}

// NewRuntimeCatalogue creates the database-time pricing resolver with a validated startup fallback.
func NewRuntimeCatalogue(initial *Catalogue, q Queries, pool *pgxpool.Pool, events analytics.Events) *RuntimeCatalogue {
	return &RuntimeCatalogue{queries: q, pool: pool, events: events, last: initial}
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
	c.last = loaded
	c.mu.Unlock()
	if previous == nil || previous.config.Version != loaded.config.Version {
		c.recordActivation(r.Context(), loaded.config.Version)
	}
	loaded.ServeHTTP(w, r)
}

func (c *RuntimeCatalogue) recordActivation(ctx context.Context, version string) {
	if c.pool == nil {
		return
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "pricing activation transaction failed", "version", version, "error", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var inserted string
	err = tx.QueryRow(ctx, `INSERT INTO pricing_activation_events(version) VALUES($1) ON CONFLICT DO NOTHING RETURNING version`, version).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "pricing activation marker failed", "version", version, "error", err)
		return
	}
	class := "non_adverse"
	_ = tx.QueryRow(ctx, "SELECT change_class FROM pricing_drafts WHERE version=$1", version).Scan(&class)
	if _, err = tx.Exec(ctx, `INSERT INTO pricing_audit(actor_id,action,version,change_class,reason,request_id) VALUES($1,'version_activated',$2,$3,'scheduled activation',$4)`, actorHash(systemActor), version, class, "activation-"+version); err != nil {
		slog.ErrorContext(ctx, "pricing activation audit failed", "version", version, "error", err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		slog.ErrorContext(ctx, "pricing activation commit failed", "version", version, "error", err)
		return
	}
	if c.events == nil {
		return
	}
	properties := map[string]any{"pricing_version": version, "change_class": class, "actor_id": systemActor, "platform": "web", "app_version": "development", "source": "api"}
	_ = c.events.Capture(systemActor, "pricing_version_activated", properties)
	_ = c.events.Capture(systemActor, "price_table_activated", properties)
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
