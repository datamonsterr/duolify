package accounts

import (
	"fmt"
	"strconv"
)

// RateLimits controls the one-hour email-code request and verification limits.
type RateLimits struct {
	RequestIP    int
	RequestEmail int
	VerifyIP     int
}

// DefaultRateLimits returns the strict production auth limits.
func DefaultRateLimits() RateLimits {
	return RateLimits{RequestIP: 20, RequestEmail: 3, VerifyIP: 60}
}

// RateLimitsFromEnv validates the auth limits before a server starts.
func RateLimitsFromEnv(getenv func(string) string) (RateLimits, error) {
	limits := DefaultRateLimits()
	for _, item := range []struct {
		name string
		dst  *int
	}{
		{"AUTH_CODE_REQUESTS_PER_IP_PER_HOUR", &limits.RequestIP},
		{"AUTH_CODE_REQUESTS_PER_EMAIL_PER_HOUR", &limits.RequestEmail},
		{"AUTH_CODE_VERIFICATIONS_PER_IP_PER_HOUR", &limits.VerifyIP},
	} {
		raw := getenv(item.name)
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			return RateLimits{}, fmt.Errorf("%s must be a positive integer", item.name)
		}
		*item.dst = value
	}
	return limits, nil
}
