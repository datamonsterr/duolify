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

// ClerkFromEnv enables SSO only when Clerk is fully configured. With no Clerk keys it
// returns a nil verifier (feature off, fail closed); a partial configuration is an
// error so a typo cannot silently disable or half-enable sign-in.
func ClerkFromEnv(getenv func(string) string) (ClerkVerifier, string, error) {
	secret, publishable := getenv("CLERK_SECRET_KEY"), getenv("CLERK_PUBLISHABLE_KEY")
	if secret == "" && publishable == "" {
		return nil, "", nil
	}
	if secret == "" || publishable == "" {
		return nil, "", errors.New("CLERK_SECRET_KEY and CLERK_PUBLISHABLE_KEY must be set together")
	}
	if !strings.HasPrefix(publishable, "pk_") {
		return nil, "", errors.New("CLERK_PUBLISHABLE_KEY must be a Clerk publishable key (pk_...)")
	}
	var parties []string
	for _, party := range strings.Split(getenv("CLERK_AUTHORIZED_PARTIES"), ",") {
		if party = strings.TrimSpace(party); party != "" {
			parties = append(parties, party)
		}
	}
	verifier, err := NewClerkVerifier(ClerkConfig{SecretKey: secret, JWTKey: getenv("CLERK_JWT_KEY"), AuthorizedParties: parties})
	return verifier, publishable, err
}
