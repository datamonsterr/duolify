// Command backup-expire is the retention job. It runs under its own list+delete
// identity and shares no credentials with the append-only backup writer.
package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := backup.Load(os.Getenv)
	if err != nil {
		return err
	}
	retention, err := backup.LoadRetention(os.Getenv)
	if err != nil {
		return err
	}
	// Replace the writer key with the expirer identity.
	cfg.S3.AccessKey, cfg.S3.SecretKey = os.Getenv("BACKUP_EXPIRE_S3_ACCESS_KEY_ID"), os.Getenv("BACKUP_EXPIRE_S3_SECRET_ACCESS_KEY")
	if cfg.S3.AccessKey == os.Getenv("BACKUP_S3_ACCESS_KEY_ID") {
		return errors.New("expirer must not reuse writer credentials")
	}
	store, err := backup.NewS3(cfg.S3)
	if err != nil {
		return err
	}
	key := os.Getenv("POSTHOG_API_KEY")
	if cfg.OptOut {
		key = ""
	}
	events, err := analytics.New(key, os.Getenv("POSTHOG_HOST"), func(context.Context, string) (bool, error) { return true, nil })
	if err != nil {
		return err
	}
	defer func() { _ = events.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	_, err = backup.Expirer{Store: store, Retention: retention, Now: time.Now, Events: events, OptOut: cfg.OptOut, Logger: slog.New(slog.NewJSONHandler(os.Stdout, nil))}.Run(ctx)
	return err
}
