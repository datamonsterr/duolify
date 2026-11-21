package s3fallback

import "testing"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func complete() map[string]string {
	return map[string]string{"X_FALLBACK_ENABLED": "true", "OLD_ENDPOINT": "https://old.example.test", "OLD_REGION": "r", "OLD_BUCKET": "old", "OLD_ACCESS_KEY_ID": "k", "OLD_SECRET_ACCESS_KEY": "s"}
}

func TestOffByDefault(t *testing.T) {
	for _, v := range []string{"", "false", "0"} {
		got, err := Load(env(map[string]string{"X_FALLBACK_ENABLED": v, "OLD_BUCKET": "old"}), "X_FALLBACK_ENABLED", "OLD", "https://new.example.test", "new")
		if got != nil || err != nil {
			t.Fatalf("%q: %v %v", v, got, err)
		}
	}
}

func TestEnabledRequiresCompleteOldStore(t *testing.T) {
	for _, drop := range []string{"OLD_ENDPOINT", "OLD_REGION", "OLD_BUCKET", "OLD_ACCESS_KEY_ID", "OLD_SECRET_ACCESS_KEY"} {
		m := complete()
		delete(m, drop)
		if _, err := Load(env(m), "X_FALLBACK_ENABLED", "OLD", "https://new.example.test", "new"); err == nil {
			t.Fatalf("missing %s accepted", drop)
		}
	}
	if got, err := Load(env(complete()), "X_FALLBACK_ENABLED", "OLD", "https://new.example.test", "new"); err != nil || got == nil || got.Bucket != "old" {
		t.Fatal(got, err)
	}
}

func TestOldStoreMustDifferFromNew(t *testing.T) {
	if _, err := Load(env(complete()), "X_FALLBACK_ENABLED", "OLD", "https://OLD.example.test/", "old"); err == nil {
		t.Fatal("same store accepted")
	}
	// Same endpoint with another bucket is a different store.
	if _, err := Load(env(complete()), "X_FALLBACK_ENABLED", "OLD", "https://old.example.test", "new"); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsBadSwitchAndEndpoint(t *testing.T) {
	m := complete()
	m["X_FALLBACK_ENABLED"] = "maybe"
	if _, err := Load(env(m), "X_FALLBACK_ENABLED", "OLD", "", ""); err == nil {
		t.Fatal("bad switch accepted")
	}
	m = complete()
	m["OLD_ENDPOINT"] = "ftp://x"
	if _, err := Load(env(m), "X_FALLBACK_ENABLED", "OLD", "", ""); err == nil {
		t.Fatal("bad endpoint accepted")
	}
}
