package backup

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"time"
)

// Class is a backup retention class derived from the object key.
type Class int

const (
	// General dumps roll off after Retention.GeneralDays.
	General Class = iota + 1
	// Financial dumps live under financial/ and roll off after Retention.FinancialDays.
	// They never shorten the live seven-year financial history in Postgres.
	Financial
)

// Retention holds the approved ADR 0011 windows: 14 and 35 days.
type Retention struct{ GeneralDays, FinancialDays int }

// DefaultRetention is the owner-approved split.
var DefaultRetention = Retention{GeneralDays: 14, FinancialDays: 35}

// LoadRetention reads optional overrides. Windows may only be lengthened.
func LoadRetention(getenv func(string) string) (Retention, error) {
	r := DefaultRetention
	for name, target := range map[string]*int{"BACKUP_GENERAL_RETENTION_DAYS": &r.GeneralDays, "BACKUP_FINANCIAL_RETENTION_DAYS": &r.FinancialDays} {
		if value := getenv(name); value != "" {
			days, err := strconv.Atoi(value)
			if err != nil {
				return Retention{}, errors.New("invalid backup retention days")
			}
			*target = days
		}
	}
	if r.GeneralDays < DefaultRetention.GeneralDays || r.FinancialDays < DefaultRetention.FinancialDays || r.FinancialDays < r.GeneralDays {
		return Retention{}, errors.New("backup retention below approved minimum")
	}
	return r, nil
}

var dumpKey = regexp.MustCompile(`^(financial/)?(\d{4}-\d{2}-\d{2})/schema-\d+\.dump\.age$`)

// ClassifyKey recognizes only dump keys; deletion-ledger and unknown keys are never expirable.
func ClassifyKey(key string) (Class, time.Time, bool) {
	m := dumpKey.FindStringSubmatch(key)
	if m == nil {
		return 0, time.Time{}, false
	}
	date, err := time.Parse(time.DateOnly, m[2])
	if err != nil {
		return 0, time.Time{}, false
	}
	if m[1] != "" {
		return Financial, date, true
	}
	return General, date, true
}

// Object is a listed backup object.
type Object struct {
	Key  string
	Size int64
}

// ExpiryStore is the only storage surface of the expirer: list and delete.
// The writer's Objects interface deliberately has neither.
type ExpiryStore interface {
	List(context.Context, string) ([]Object, error)
	Delete(context.Context, string) error
}

// ExpiryResult summarizes one run.
type ExpiryResult struct {
	Deleted int
	Bytes   int64
}

// Expirer deletes dumps older than their class window. It runs as its own
// identity and job, never inside the append-only writer.
type Expirer struct {
	Store     ExpiryStore
	Retention Retention
	Now       func() time.Time
	Events    interface {
		Capture(string, string, map[string]any) error
	}
	OptOut bool
	Logger *slog.Logger
}

// Run lists dump prefixes only. A dump dated D is kept through D+N days.
func (e Expirer) Run(ctx context.Context) (result ExpiryResult, err error) {
	started := time.Now()
	defer func() {
		if err != nil {
			emitOutcome(e.Events, e.OptOut, false, result.Bytes, time.Since(started))
		} else if e.Logger != nil {
			e.Logger.InfoContext(ctx, "backup retention completed", "deleted", result.Deleted, "bytes", result.Bytes)
		}
	}()
	if e.Store == nil || e.Now == nil || e.Retention.GeneralDays < 1 || e.Retention.FinancialDays < 1 {
		return result, errors.New("backup expirer misconfigured")
	}
	now := e.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	for _, prefix := range []string{"20", "financial/20"} {
		objects, listErr := e.Store.List(ctx, prefix)
		if listErr != nil {
			return result, errors.New("backup retention listing failed")
		}
		for _, object := range objects {
			class, date, ok := ClassifyKey(object.Key)
			if !ok {
				continue
			}
			days := e.Retention.GeneralDays
			if class == Financial {
				days = e.Retention.FinancialDays
			}
			if !date.AddDate(0, 0, days).Before(today) {
				continue
			}
			if deleteErr := e.Store.Delete(ctx, object.Key); deleteErr != nil {
				return result, errors.New("backup retention delete failed")
			}
			result.Deleted++
			result.Bytes += object.Size
		}
	}
	return result, nil
}
