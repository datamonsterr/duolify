package player

import (
	"testing"
	"time"
)

func TestManifestCacheIsBoundedKeyedAndExpires(t *testing.T) {
	c := newManifestCache(2, time.Minute)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	a, b, d := manifestCacheKey("lessons/a/manifest.json", 1), manifestCacheKey("lessons/b/manifest.json", 1), manifestCacheKey("lessons/d/manifest.json", 1)
	c.put(a, ObjectManifest{Version: 1})
	c.put(b, ObjectManifest{Version: 1})
	if _, ok := c.get(a); !ok { // a is now most recent
		t.Fatal("expected hit")
	}
	c.put(d, ObjectManifest{Version: 1}) // evicts b, the least recently used
	if _, ok := c.get(b); ok {
		t.Fatal("least recently used entry survived the bound")
	}
	if _, ok := c.get(a); !ok {
		t.Fatal("recent entry evicted")
	}
	if _, ok := c.get(manifestCacheKey("lessons/a/manifest.json", 2)); ok {
		t.Fatal("a new version must not hit an old entry")
	}
	now = now.Add(time.Minute)
	if _, ok := c.get(a); ok {
		t.Fatal("expired entry served")
	}
}

func TestManifestCacheDisabledAndCanonicalUUID(t *testing.T) {
	c := newManifestCache(4, -1)
	c.put("k", ObjectManifest{Version: 1})
	if _, ok := c.get("k"); ok {
		t.Fatal("disabled cache stored an entry")
	}
	if got, ok := CanonicalUUID("F3333333-3333-4333-8333-333333333333"); !ok || got != "f3333333-3333-4333-8333-333333333333" {
		t.Fatal(got, ok)
	}
	for _, bad := range []string{"", "not-a-uuid", "f3333333333343338333333333333333", "{f3333333-3333-4333-8333-333333333333}", "f3333333-3333-4333-8333-333333333333x", "f3333333-3333-4333-8333-33333333333g"} {
		if _, ok := CanonicalUUID(bad); ok {
			t.Fatal("accepted", bad)
		}
	}
}
