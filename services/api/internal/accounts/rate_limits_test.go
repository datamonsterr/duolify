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
