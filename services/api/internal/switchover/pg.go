package switchover

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// replicator is the database side of a cutover; the real one talks to both Postgres instances.
type replicator interface {
	// SetReadOnly makes the old database refuse writes, ends other client sessions and returns its WAL position.
	SetReadOnly(ctx context.Context) (lsn string, err error)
	ClearReadOnly(ctx context.Context) error
	// WaitLSN returns once the subscriber has applied everything up to lsn.
	WaitLSN(ctx context.Context, lsn string) error
	// SyncSequences sets every target sequence to the publisher value plus the margin.
	SyncSequences(ctx context.Context) error
}

type pgReplicator struct {
	source, target, subscription string
	margin                       int64
}

func connect(ctx context.Context, url string) (*pgx.Conn, error) {
	cfg, err := parse(url)
	if err != nil {
		return nil, err
	}
	// The old database may already default new sessions to read-only; the gate must still be able to undo that.
	cfg.RuntimeParams["default_transaction_read_only"] = "off"
	return pgx.ConnectConfig(ctx, cfg)
}

func (r *pgReplicator) SetReadOnly(ctx context.Context) (string, error) {
	conn, err := connect(ctx, r.source)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	var name string
	if err = conn.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil {
		return "", err
	}
	if _, err = conn.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" SET default_transaction_read_only = on"); err != nil {
		return "", err
	}
	// New sessions are read-only now; end the existing ones (not replication walsenders) and wait until
	// none can still be committing, so the LSN read below covers every write.
	const others = `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND backend_type='client backend'`
	for {
		if _, err = conn.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND backend_type='client backend'`); err != nil {
			return "", err
		}
		var n int
		if err = conn.QueryRow(ctx, others).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	var lsn string
	err = conn.QueryRow(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&lsn)
	return lsn, err
}

func (r *pgReplicator) ClearReadOnly(ctx context.Context) error {
	conn, err := connect(ctx, r.source)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	var name string
	if err = conn.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil {
		return err
	}
	_, err = conn.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" RESET default_transaction_read_only")
	return err
}

// WaitLSN polls the publisher's slot: confirmed_flush_lsn is the position the subscriber has applied and flushed.
func (r *pgReplicator) WaitLSN(ctx context.Context, lsn string) error {
	conn, err := connect(ctx, r.source)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	for {
		var caught *bool
		err = conn.QueryRow(ctx, "SELECT confirmed_flush_lsn >= $1::pg_lsn FROM pg_replication_slots WHERE slot_name=$2 AND slot_type='logical'", lsn, r.subscription).Scan(&caught)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("replication slot %q not found on the old database", r.subscription)
		case err != nil && ctx.Err() == nil:
			return err
		case err == nil && caught != nil && *caught:
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("subscriber has not reached %s: %w", lsn, ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (r *pgReplicator) SyncSequences(ctx context.Context) error {
	src, err := connect(ctx, r.source)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close(context.WithoutCancel(ctx)) }()
	dst, err := connect(ctx, r.target)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close(context.WithoutCancel(ctx)) }()
	rows, err := src.Query(ctx, `SELECT schemaname, sequencename, last_value FROM pg_sequences WHERE last_value IS NOT NULL AND schemaname NOT IN ('pg_catalog','information_schema')`)
	if err != nil {
		return err
	}
	type seq struct {
		schema, name string
		last         int64
	}
	seqs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (s seq, err error) {
		err = row.Scan(&s.schema, &s.name, &s.last)
		return
	})
	if err != nil {
		return err
	}
	// Never lower a sequence and never exceed its maximum.
	const set = `SELECT setval($1::regclass, LEAST(GREATEST($2::bigint, COALESCE(cur.last_value,0)), seq.max_value), true)
FROM pg_sequences seq LEFT JOIN pg_sequences cur ON cur.schemaname=seq.schemaname AND cur.sequencename=seq.sequencename
WHERE seq.schemaname=$3 AND seq.sequencename=$4`
	batch := &pgx.Batch{}
	for _, s := range seqs {
		batch.Queue(set, pgx.Identifier{s.schema, s.name}.Sanitize(), s.last+r.margin, s.schema, s.name)
	}
	results := dst.SendBatch(ctx, batch)
	for _, s := range seqs {
		if _, err = results.Exec(); err != nil {
			_ = results.Close()
			return fmt.Errorf("setval %s.%s: %w", s.schema, s.name, err)
		}
	}
	return results.Close()
}
