package credits

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func TestStripeRawSignatureBoundaries(t *testing.T) {
	now := time.Unix(1800000000, 0)
	body := []byte("{\n  \"id\": \"evt_fixture\"\n}")
	sign := func(at time.Time) string {
		stamp := fmt.Sprint(at.Unix())
		mac := hmac.New(sha256.New, []byte("fixture-secret"))
		_, _ = mac.Write([]byte(stamp + "."))
		_, _ = mac.Write(body)
		return "t=" + stamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	}
	valid := sign(now)
	for _, header := range []string{valid, "v0=untrusted," + valid, valid + ",v1=bad", "t=1800000000,v1=bad," + valid[len("t=1800000000,"):], sign(now.Add(-5 * time.Minute)), sign(now.Add(5 * time.Minute))} {
		if err := verifyStripeSignature(body, header, "fixture-secret", now); err != nil {
			t.Fatalf("valid rotation/window refused: %s: %v", header, err)
		}
	}
	for _, tc := range []struct {
		raw            []byte
		header, secret string
	}{{body, "", "fixture-secret"}, {body, valid, ""}, {body, valid, "wrong"}, {body, "t=invalid,v1=deadbeef", "fixture-secret"}, {body, "t=1800000000,v0=" + valid[len("t=1800000000,v1="):], "fixture-secret"}, {append(append([]byte{}, body...), ' '), valid, "fixture-secret"}, {body, sign(now.Add(-301 * time.Second)), "fixture-secret"}, {body, sign(now.Add(301 * time.Second)), "fixture-secret"}, {body, valid + ",t=1800000001", "fixture-secret"}} {
		if verifyStripeSignature(tc.raw, tc.header, tc.secret, now) == nil {
			t.Fatalf("invalid signature accepted: %s", tc.header)
		}
	}
}
func TestPaymentsLiveGateAndDefaultOff(t *testing.T) {
	s := NewService(nil, nil)
	for _, options := range []PaymentOptions{{Mode: "live"}, {Mode: "live", LiveEnabled: true, WebhookSecret: "fixture"}, {Mode: "test", WebhookSecret: "fixture"}, {Mode: "unknown"}} {
		if s.ConfigurePayments(options) == nil {
			t.Fatalf("unsafe configuration: %+v", options)
		}
	}
	if err := s.ConfigurePayments(PaymentOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, env := range []map[string]string{{}, {"STRIPE_MODE": "disabled", "STRIPE_LIVE_ENABLED": "false"}} {
		if err := s.ConfigurePaymentEnvironment(func(key string) string { return env[key] }); err != nil {
			t.Fatal(err)
		}
	}
	for _, env := range []map[string]string{{"STRIPE_MODE": "live"}, {"STRIPE_LIVE_ENABLED": "true"}, {"STRIPE_LIVE_ENABLED": "invalid"}, {"STRIPE_MODE": "test", "STRIPE_WEBHOOK_SECRET": "fixture"}} {
		if s.ConfigurePaymentEnvironment(func(key string) string { return env[key] }) == nil {
			t.Fatalf("unsafe deployment environment accepted: %v", env)
		}
	}
}
