// Package s3fallback loads the temporary read-only "old store" used while objects
// move between buckets (ADR 0014). It is off unless explicitly enabled.
package s3fallback

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Store is the old, read-only object store.
type Store struct{ Endpoint, Region, Bucket, AccessKey, SecretKey string }

// Load reads enabledVar and, when true, the <oldPrefix>_ENDPOINT, _REGION, _BUCKET,
// _ACCESS_KEY_ID and _SECRET_ACCESS_KEY variables. It returns nil when the fallback is
// off. The old store must differ from the new one (endpoint and bucket), or reads would
// loop on the store being migrated to.
func Load(getenv func(string) string, enabledVar, oldPrefix, newEndpoint, newBucket string) (*Store, error) {
	raw := strings.TrimSpace(getenv(enabledVar))
	if raw == "" {
		return nil, nil
	}
	on, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, errors.New("invalid " + enabledVar)
	}
	if !on {
		return nil, nil
	}
	old := &Store{Endpoint: getenv(oldPrefix + "_ENDPOINT"), Region: getenv(oldPrefix + "_REGION"), Bucket: getenv(oldPrefix + "_BUCKET"), AccessKey: getenv(oldPrefix + "_ACCESS_KEY_ID"), SecretKey: getenv(oldPrefix + "_SECRET_ACCESS_KEY")}
	for suffix, value := range map[string]string{"_ENDPOINT": old.Endpoint, "_REGION": old.Region, "_BUCKET": old.Bucket, "_ACCESS_KEY_ID": old.AccessKey, "_SECRET_ACCESS_KEY": old.SecretKey} {
		if value == "" {
			return nil, errors.New(enabledVar + " requires " + oldPrefix + suffix)
		}
	}
	endpoint, err := url.Parse(old.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil {
		return nil, errors.New("invalid " + oldPrefix + "_ENDPOINT")
	}
	if sameEndpoint(old.Endpoint, newEndpoint) && old.Bucket == newBucket {
		return nil, errors.New(oldPrefix + " store must differ from the new store")
	}
	return old, nil
}

func sameEndpoint(a, b string) bool {
	return strings.TrimRight(strings.ToLower(a), "/") == strings.TrimRight(strings.ToLower(b), "/")
}
