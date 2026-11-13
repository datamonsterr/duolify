package backup

import (
	"errors"
	"net/url"
	"strings"

	"filippo.io/age"
)

// ErrDrillDisabled means the scheduled drill has no owner credentials. Callers
// skip with a notice; absence of secrets is never a failure.
var ErrDrillDisabled = errors.New("restore drill disabled: owner backup credentials are not configured")

// DrillConfig is everything the scheduled entrypoint needs. The age identity
// (private key) belongs to the owner and never to the backup writer.
type DrillConfig struct {
	S3          S3Options
	Identity    age.Identity
	OptOut      bool
	Container   string // throwaway Postgres container name (docker exec target)
	DatabaseURL string // loopback URL of that container's published port
	RedisURL    string // loopback throwaway Redis rebuilt from the restored Postgres
	PGUser      string
}

// LoadDrill requires the restorer identity, the age private key and a throwaway
// Postgres. Anything missing disables the drill; anything malformed is an error.
func LoadDrill(getenv func(string) string) (DrillConfig, error) {
	cfg := DrillConfig{S3: S3Options{
		Endpoint: getenv("BACKUP_S3_ENDPOINT"), Region: getenv("BACKUP_S3_REGION"), Bucket: getenv("BACKUP_S3_BUCKET"),
		AccessKey: getenv("BACKUP_RESTORE_S3_ACCESS_KEY_ID"), SecretKey: getenv("BACKUP_RESTORE_S3_SECRET_ACCESS_KEY"),
	}, Container: getenv("DRILL_PG_CONTAINER"), DatabaseURL: getenv("DRILL_DATABASE_URL"), RedisURL: getenv("DRILL_REDIS_URL"), PGUser: getenv("DRILL_PG_USER"), OptOut: strings.EqualFold(getenv("ANALYTICS_OPT_OUT"), "true")}
	secret := getenv("BACKUP_AGE_IDENTITY")
	for _, value := range []string{cfg.S3.Endpoint, cfg.S3.Bucket, cfg.S3.AccessKey, cfg.S3.SecretKey, secret, cfg.Container, cfg.DatabaseURL, cfg.RedisURL} {
		if value == "" {
			return DrillConfig{}, ErrDrillDisabled
		}
	}
	identity, err := age.ParseX25519Identity(secret)
	if err != nil {
		return DrillConfig{}, errors.New("invalid BACKUP_AGE_IDENTITY")
	}
	cfg.Identity = identity
	if cfg.PGUser == "" {
		cfg.PGUser = "postgres"
	}
	for _, raw := range []string{cfg.DatabaseURL, cfg.RedisURL} {
		u, err := url.Parse(raw)
		if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
			return DrillConfig{}, errors.New("drill Postgres and Redis must be loopback throwaways")
		}
	}
	if cfg.S3.AccessKey == getenv("BACKUP_S3_ACCESS_KEY_ID") {
		return DrillConfig{}, errors.New("restore drill must not use the backup writer credentials")
	}
	return cfg, nil
}
