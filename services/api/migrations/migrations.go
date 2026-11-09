// Package migrations embeds the Goose schema so migrate works in the distroless image.
package migrations

import (
	"context"
	"embed"
	"errors"

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
