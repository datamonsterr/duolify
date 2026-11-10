package accounts

import (
	"fmt"
	"strconv"
)

// RateLimits controls the one-hour email-code request and verification limits.
type RateLimits struct {
	RequestIP        int
	RequestEmail     int
	VerifyIP         int
	TrustedProxyHops int
	DeliveryEmail    int
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
	if raw := getenv("AUTH_TRUSTED_PROXY_HOPS"); raw != "" {
		hops, err := strconv.Atoi(raw)
		if err != nil || hops < 0 || hops > 32 {
			return RateLimits{}, fmt.Errorf("AUTH_TRUSTED_PROXY_HOPS must be between 0 and 32")
		}
		limits.TrustedProxyHops = hops
	}
	if raw := getenv("AUTH_CODE_DELIVERIES_PER_EMAIL_PER_HOUR"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			return RateLimits{}, fmt.Errorf("AUTH_CODE_DELIVERIES_PER_EMAIL_PER_HOUR must be positive")
		}
		limits.DeliveryEmail = value
	}
	return limits, nil
}

func (r RateLimits) emailDeliveries() int {
	if r.DeliveryEmail > 0 {
		return r.DeliveryEmail
	}
	return 10
}
