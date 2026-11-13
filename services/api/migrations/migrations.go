// Package migrations embeds the Goose schema so migrate works in the distroless image.
package migrations

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var schema embed.FS

// Up applies application migrations. Running twice is safe.
func Up(ctx context.Context, databaseURL string) (result error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("invalid DATABASE_URL")
	}
	db := stdlib.OpenDB(*config.ConnConfig)
	defer func() { result = errors.Join(result, db.Close()) }()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, schema)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

// Versions lists every embedded migration version, so restore drills can prove
// a recovered database has applied the complete current schema.
func Versions() ([]int64, error) {
	entries, err := fs.ReadDir(schema, ".")
	if err != nil {
		return nil, err
	}
	var versions []int64
	for _, entry := range entries {
		prefix, _, _ := strings.Cut(entry.Name(), "_")
		prefix = strings.TrimSuffix(prefix, ".sql")
		if version, err := strconv.ParseInt(prefix, 10, 64); err == nil {
			versions = append(versions, version)
		}
	}
	slices.Sort(versions)
	return versions, nil
}
