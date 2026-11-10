package accounts

import (
	"errors"
	"log/slog"
	"strings"
)

// AuthFromEnv defaults to production; only an explicit development environment
// permits the local log mailer and development placeholder signing key.
func AuthFromEnv(getenv func(string) string, logger *slog.Logger) (string, Mailer, error) {
	secret := getenv("JWT_SECRET")
	if len(secret) < 32 {
		return "", nil, errors.New("JWT_SECRET must contain at least 32 bytes")
	}
	if getenv("DUOLIFY_ENV") == "development" {
		return secret, LogMailer{Logger: logger}, nil
	}
	unique := map[rune]bool{}
	for _, r := range secret {
		unique[r] = true
	}
	if strings.HasPrefix(secret, "change-me") || len(unique) < 16 {
		return "", nil, errors.New("production JWT_SECRET requires a strong random secret of at least 32 bytes")
	}
	if getenv("RESEND_API_KEY") == "" || getenv("AUTH_MAIL_FROM") == "" {
		return "", nil, errors.New("production auth requires RESEND_API_KEY and AUTH_MAIL_FROM")
	}
	return secret, ResendMailer{APIKey: getenv("RESEND_API_KEY"), From: getenv("AUTH_MAIL_FROM")}, nil
}
