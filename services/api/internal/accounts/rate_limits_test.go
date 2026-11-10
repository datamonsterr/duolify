package accounts

import "testing"

func TestRateLimitsFromEnvKeepsStrictProductionDefaults(t *testing.T) {
	limits, err := RateLimitsFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if limits != (RateLimits{RequestIP: 20, RequestEmail: 3, VerifyIP: 60}) || limits != DefaultRateLimits() {
		t.Fatalf("production auth limits changed: %+v", limits)
	}
}

func TestRateLimitsFromEnvRejectsInvalidValues(t *testing.T) {
	for _, envVar := range []string{
		"AUTH_CODE_REQUESTS_PER_IP_PER_HOUR",
		"AUTH_CODE_REQUESTS_PER_EMAIL_PER_HOUR",
		"AUTH_CODE_VERIFICATIONS_PER_IP_PER_HOUR",
		"AUTH_CODE_DELIVERIES_PER_EMAIL_PER_HOUR",
	} {
		for _, value := range []string{"0", "-1", "-100", "many", "1.5", "abc"} {
			t.Run(envVar+"="+value, func(t *testing.T) {
				_, err := RateLimitsFromEnv(func(name string) string {
					if name == envVar {
						return value
					}
					return ""
				})
				if err == nil {
					t.Fatalf("accepted invalid limit %s=%q", envVar, value)
				}
			})
		}
	}
}

func TestTrustedProxyHopConfiguration(t *testing.T) {
	for _, raw := range []string{"-1", "33", "one", "1.5"} {
		if _, err := RateLimitsFromEnv(func(key string) string {
			if key == "AUTH_TRUSTED_PROXY_HOPS" {
				return raw
			}
			return ""
		}); err == nil {
			t.Errorf("accepted invalid trusted-hop count %q", raw)
		}
	}
	limits, err := RateLimitsFromEnv(func(key string) string {
		switch key {
		case "AUTH_TRUSTED_PROXY_HOPS":
			return "2"
		case "AUTH_CODE_DELIVERIES_PER_EMAIL_PER_HOUR":
			return "15"
		default:
			return ""
		}
	})
	if err != nil || limits.TrustedProxyHops != 2 || limits.DeliveryEmail != 15 {
		t.Fatalf("configured limits: %+v, %v", limits, err)
	}
}

func TestRateLimitsFromEnvAcceptsValidValues(t *testing.T) {
	limits, err := RateLimitsFromEnv(func(name string) string {
		switch name {
		case "AUTH_CODE_REQUESTS_PER_IP_PER_HOUR":
			return "100"
		case "AUTH_CODE_REQUESTS_PER_EMAIL_PER_HOUR":
			return "50"
		case "AUTH_CODE_VERIFICATIONS_PER_IP_PER_HOUR":
			return "200"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if limits != (RateLimits{RequestIP: 100, RequestEmail: 50, VerifyIP: 200}) {
		t.Fatalf("unexpected limits: %+v", limits)
	}
}
