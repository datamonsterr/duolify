//go:build !integration

package sharing_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

func TestAssetPurgePeriodDefaultsToThirtyDaysAndIsBounded(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{{"", 30}, {"45", 45}, {"15", 15}, {"14", 30}, {"0", 30}, {"-3", 30}, {"abc", 30}, {"3651", 30}, {"3650", 3650}}
	for _, c := range cases {
		got := sharing.ConfigFromEnv(func(k string) string {
			if k == "SHARING_ASSET_PURGE_DAYS" {
				return c.raw
			}
			return ""
		}).AssetPurgeDays
		if got != c.want {
			t.Errorf("SHARING_ASSET_PURGE_DAYS=%q = %d, want %d", c.raw, got, c.want)
		}
	}
}

func TestReleaseApprovalsAreOffUnlessExactlyTrueAndNeverImplyTheFlag(t *testing.T) {
	for _, raw := range []string{"", "1", "TRUE", "yes", "true "} {
		c := sharing.ConfigFromEnv(func(string) string { return raw })
		if c.ReleaseLegalApproved || c.ReleaseCDNPolicyApproved || c.MarketplaceEnabled {
			t.Errorf("%q enabled something: %+v", raw, c)
		}
	}
	c := sharing.ConfigFromEnv(func(k string) string {
		if k == "SHARING_RELEASE_LEGAL_APPROVED" || k == "SHARING_RELEASE_CDN_POLICY_APPROVED" {
			return "true"
		}
		return ""
	})
	if !c.ReleaseLegalApproved || !c.ReleaseCDNPolicyApproved || c.MarketplaceEnabled {
		t.Fatalf("approvals must not turn the Marketplace on: %+v", c)
	}
}

func TestPurgeAtAddsWholeDays(t *testing.T) {
	now := time.Date(2026, 11, 20, 8, 30, 0, 0, time.UTC)
	if got := sharing.PurgeAt(now, 30); !got.Equal(now.Add(30 * 24 * time.Hour)) {
		t.Fatalf("PurgeAt = %v", got)
	}
	if got := sharing.PurgeAt(now, 0); !got.Equal(now) {
		t.Fatalf("zero days = %v", got)
	}
}

func TestPrefixOfKeyFindsTheVersionFolderOnly(t *testing.T) {
	id := "11111111-2222-4333-8444-555555555555"
	if got := sharing.PinPrefix(id); got != "lessons/published/"+id+"/" {
		t.Fatalf("PinPrefix = %q", got)
	}
	cases := []struct {
		key  string
		want string
		ok   bool
	}{
		{"lessons/published/" + id + "/0-1/manifest.json", "lessons/published/" + id + "/", true},
		{"lessons/published/" + id + "/0-1", "lessons/published/" + id + "/", true},
		{"lessons/published/check/0-1/manifest.json", "", false},
		{"lessons/private/" + id + "/x", "", false},
		{"lessons/published/" + id, "", false},
		{"", "", false},
		{"../lessons/published/" + id + "/x", "", false},
	}
	for _, c := range cases {
		got, ok := sharing.PrefixOfKey(c.key)
		if got != c.want || ok != c.ok {
			t.Errorf("PrefixOfKey(%q) = %q,%v want %q,%v", c.key, got, ok, c.want, c.ok)
		}
	}
}

func TestLedgerRefIsAStableHashThatDoesNotEchoTheID(t *testing.T) {
	id := "11111111-2222-4333-8444-555555555555"
	a, b := sharing.LedgerRef(id), sharing.LedgerRef(id)
	if a != b || len(a) != 64 || strings.Contains(a, "1111") || a == sharing.LedgerRef(id+"x") {
		t.Fatalf("LedgerRef = %q %q", a, b)
	}
	// sha256("abc") pins the algorithm for the restore side, which hashes in SQL.
	if got := sharing.LedgerRef("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("LedgerRef(abc) = %s", got)
	}
}

func TestReadinessListsEveryGateInStableOrderAndNeverSetsTheFlag(t *testing.T) {
	r := sharing.EvaluateReadiness(sharing.DefaultConfig(), true)
	keys := make([]string, 0, len(r.Criteria))
	for _, c := range r.Criteria {
		keys = append(keys, c.Key)
		if c.Detail == "" {
			t.Errorf("%s has no detail", c.Key)
		}
	}
	want := []string{"approval.cdn_policy", "approval.legal", "caps.enroll", "caps.invite", "caps.publish", "caps.report", "caps.review", "erasure.attribution", "noindex", "retention.assets"}
	if !slices.Equal(keys, want) {
		t.Fatalf("order = %v", keys)
	}
	if r.Ready || r.MarketplaceEnabled {
		t.Fatalf("defaults must be unready and off: %+v", r)
	}
	cfg := sharing.DefaultConfig()
	cfg.ReleaseLegalApproved, cfg.ReleaseCDNPolicyApproved = true, true
	if r = sharing.EvaluateReadiness(cfg, true); !r.Ready || r.MarketplaceEnabled {
		t.Fatalf("all gates met: %+v", r)
	}
	if r = sharing.EvaluateReadiness(cfg, false); r.Ready {
		t.Fatal("erasure that does not anonymize attribution must block release")
	}
}

func TestReadinessTreatsAZeroCapAsNotEnforced(t *testing.T) {
	caps := map[string]func(*sharing.Config){
		"caps.enroll":  func(c *sharing.Config) { c.MaxEnrollmentsPerDay = 0 },
		"caps.invite":  func(c *sharing.Config) { c.InviteSendsPerDay = 0 },
		"caps.publish": func(c *sharing.Config) { c.MarketplaceMaxListingsPerOwner = 0 },
		"caps.report":  func(c *sharing.Config) { c.ReportMaxPerDay = 0 },
		"caps.review":  func(c *sharing.Config) { c.ReviewMaxPerDay = 0 },
	}
	keys := make([]string, 0, len(caps))
	for k := range caps {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, key := range keys {
		cfg := sharing.DefaultConfig()
		cfg.ReleaseLegalApproved, cfg.ReleaseCDNPolicyApproved = true, true
		caps[key](&cfg)
		r := sharing.EvaluateReadiness(cfg, true)
		if r.Ready {
			t.Errorf("%s at zero still ready", key)
		}
		for _, c := range r.Criteria {
			if (c.Key == key) == c.Met && strings.HasPrefix(c.Key, "caps.") {
				t.Errorf("%s: criterion %s met=%v", key, c.Key, c.Met)
			}
		}
	}
}
