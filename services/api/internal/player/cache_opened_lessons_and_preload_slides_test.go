package player

import "testing"

func TestCacheChecksumAndAssetBoundary(t *testing.T) {
	if checksum([]byte("hello")) != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatal("incorrect SHA-256")
	}
	for _, ref := range []string{"https://evil.test/lessons/a.png", "https://assets.example.test/../secrets", "https://assets.example.test/lessons/a.png?token=private", "https://assets.example.test/lessons/%2e%2e/secret"} {
		if _, err := assetKey(ref, "https://assets.example.test"); err == nil {
			t.Fatal("unsafe asset key accepted", ref)
		}
	}
	if key, err := assetKey("https://assets.example.test/lessons/v1/star.png", "https://assets.example.test"); err != nil || key != "lessons/v1/star.png" {
		t.Fatal(key, err)
	}
}
