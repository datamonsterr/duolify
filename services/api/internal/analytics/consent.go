package analytics

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ConsentResolver returns true only for a known learner who permits analytics.
type ConsentResolver func(context.Context, string) (bool, error)

// DatabaseConsent reads the current preference. Missing learners fail closed.
func DatabaseConsent(pool *pgxpool.Pool) ConsentResolver {
	return func(ctx context.Context, id string) (bool, error) {
		// Reserved operations identity has no learner or personal preferences.
		// Global opt-out still disables the client before this resolver is used.
		if id == "00000000-0000-4000-8000-000000000011" {
			return true, nil
		}
		if pool == nil {
			return false, errors.New("consent database unavailable")
		}
		var allowed bool
		err := pool.QueryRow(ctx, "SELECT NOT analytics_opt_out FROM learner_profiles WHERE learner_id=$1::uuid", id).Scan(&allowed)
		return allowed, err
	}
}

type consentGate struct {
	Events
	resolver ConsentResolver
	mu       sync.Mutex
	denied   map[string]time.Time
}

// WithConsent gates any event sink, including test doubles. Only denials are
// cached (five seconds, at most 4096 identities), so an opt-out takes effect on
// the very next Capture. An absent, timed-out or failing resolver denies delivery.
func WithConsent(events Events, resolver ConsentResolver) Events {
	return &consentGate{Events: events, resolver: resolver, denied: map[string]time.Time{}}
}

func (g *consentGate) Capture(id, event string, properties map[string]any) error {
	if g.resolver == nil {
		return nil
	}
	g.mu.Lock()
	until := g.denied[id]
	g.mu.Unlock()
	if time.Now().Before(until) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	allowed, err := g.resolver(ctx, id)
	if err != nil || ctx.Err() != nil || !allowed {
		g.mu.Lock()
		if len(g.denied) >= 4096 {
			clear(g.denied)
		}
		g.denied[id] = time.Now().Add(5 * time.Second)
		g.mu.Unlock()
		return nil
	}
	return g.Events.Capture(id, event, properties)
}
