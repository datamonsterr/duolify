package backup

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"

	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres uses a pinned pg_dump executable in the worker image.
type Postgres struct {
	Pool        *pgxpool.Pool
	DatabaseURL string
}

// SchemaVersion reads the current applied Goose version.
func (p Postgres) SchemaVersion(ctx context.Context) (int64, error) {
	var version int64
	err := p.Pool.QueryRow(ctx, "SELECT coalesce(max(version_id),0) FROM goose_db_version WHERE is_applied").Scan(&version)
	return version, err
}

// Dump streams a consistent custom-format archive; connection secrets are passed
// through the environment rather than process arguments or diagnostic output.
func (p Postgres) Dump(ctx context.Context, destination io.Writer) error {
	// Nightly expiry cleanup keeps inactive learners' confirmed drafts bounded too.
	if err := p.PurgeExpiredBriefs(ctx); err != nil {
		return errors.New("brief retention cleanup failed")
	}

	config, err := pgxpool.ParseConfig(p.DatabaseURL)
	if err != nil {
		return errors.New("invalid dump database configuration")
	}
	parsed, err := url.Parse(p.DatabaseURL)
	if err != nil {
		return errors.New("invalid dump database configuration")
	}
	query := parsed.Query()
	command := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--no-owner", "--no-acl", "--no-password")
	// libpq defaults do not expand a URI supplied through PGDATABASE. Explicit
	// PGUSER also avoids getpwuid failures for the non-root numeric image user.
	// Keep connection secrets out of argv, and do not inherit unrelated PG*
	// variables that could point pg_dump at a different database or service.
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"PGHOST=" + config.ConnConfig.Host, "PGPORT=" + strconv.Itoa(int(config.ConnConfig.Port)),
		"PGUSER=" + config.ConnConfig.User, "PGPASSWORD=" + config.ConnConfig.Password, "PGDATABASE=" + config.ConnConfig.Database,
	}
	for param, env := range map[string]string{
		"sslmode": "PGSSLMODE", "sslrootcert": "PGSSLROOTCERT", "sslcert": "PGSSLCERT", "sslkey": "PGSSLKEY",
		"connect_timeout": "PGCONNECT_TIMEOUT", "application_name": "PGAPPNAME", "options": "PGOPTIONS",
		"target_session_attrs": "PGTARGETSESSIONATTRS", "channel_binding": "PGCHANNELBINDING",
	} {
		if value := query.Get(param); value != "" {
			command.Env = append(command.Env, env+"="+value)
		}
	}
	command.Stdout = destination
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return errors.New("pg_dump failed")
	}
	return nil
}

// PurgeExpiredBriefs applies personal-data retention before either local or packaged dumps.
func (p Postgres) PurgeExpiredBriefs(ctx context.Context) error {
	return db.New(p.Pool).PurgeExpiredCourseBriefs(ctx)
}
