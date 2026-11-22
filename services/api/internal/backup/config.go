package backup

import (
	"errors"
	"strconv"
)

// Runtime contains shared deletion-ledger storage settings; only the worker uses
// Recipient to encrypt database archives. Serve needs no recovery private key.
type Runtime struct {
	S3        S3Options
	Recipient string
	OptOut    bool
	// Versioned is false when BACKUP_S3_VERSIONING=none (Cloudflare R2 has no object versioning): object
	// stores then return no version ids, and protected asset copies are pinned by checksum alone.
	Versioned bool
}

// Load requires explicit writer keys and refuses application-credential reuse.
func Load(getenv func(string) string) (Runtime, error) {
	cfg := Runtime{S3: S3Options{
		Endpoint: getenv("BACKUP_S3_ENDPOINT"), Region: getenv("BACKUP_S3_REGION"), Bucket: getenv("BACKUP_S3_BUCKET"), ApplicationBucket: getenv("S3_BUCKET"), AccessKey: getenv("BACKUP_S3_ACCESS_KEY_ID"), SecretKey: getenv("BACKUP_S3_SECRET_ACCESS_KEY"),
	}, Recipient: getenv("BACKUP_AGE_RECIPIENT"), Versioned: true}
	switch getenv("BACKUP_S3_VERSIONING") {
	case "", "enabled":
	case "none":
		cfg.Versioned = false
	default:
		return Runtime{}, errors.New("invalid BACKUP_S3_VERSIONING (want enabled or none)")
	}
	if value := getenv("ANALYTICS_OPT_OUT"); value != "" {
		var err error
		cfg.OptOut, err = strconv.ParseBool(value)
		if err != nil {
			return Runtime{}, errors.New("invalid ANALYTICS_OPT_OUT")
		}
	}
	if cfg.S3.AccessKey != "" && cfg.S3.AccessKey == getenv("S3_ACCESS_KEY_ID") {
		return Runtime{}, errors.New("backup writer must use separate credentials")
	}
	return cfg, nil
}
