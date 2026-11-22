package player

import (
	"container/list"
	"context"
	"strconv"
	"sync"
	"time"
)

const (
	defaultManifestCacheEntries = 256
	defaultManifestCacheTTL     = 10 * time.Minute
)

// manifestCache is a small bounded LRU of decoded, validated manifests. Entries are
// keyed by the immutable manifest object key plus the version stored in Postgres, so
// a republished manifest (new key or version) can never be served from an old entry.
// It holds learner-independent content only: authorization always runs in Postgres
// before the cache is consulted. The TTL bounds how long an out-of-band change of a
// stored object can go unnoticed; after it expires the next read re-fetches and
// re-validates the object.
type manifestCache struct {
	mu    sync.Mutex
	max   int
	ttl   time.Duration
	now   func() time.Time
	order *list.List
	items map[string]*list.Element
}

type manifestEntry struct {
	key      string
	manifest ObjectManifest
	expires  time.Time
}

func newManifestCache(limit int, ttl time.Duration) *manifestCache {
	if limit <= 0 {
		limit = defaultManifestCacheEntries
	}
	if ttl == 0 {
		ttl = defaultManifestCacheTTL
	}
	return &manifestCache{max: limit, ttl: ttl, now: time.Now, order: list.New(), items: map[string]*list.Element{}}
}

func manifestCacheKey(objectKey string, version int) string {
	return strconv.Itoa(version) + "\x00" + objectKey
}

func (c *manifestCache) get(key string) (ObjectManifest, bool) {
	if c == nil || c.ttl < 0 {
		return ObjectManifest{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[key]
	if !ok {
		return ObjectManifest{}, false
	}
	entry := element.Value.(*manifestEntry)
	if !c.now().Before(entry.expires) {
		c.order.Remove(element)
		delete(c.items, key)
		return ObjectManifest{}, false
	}
	c.order.MoveToFront(element)
	return entry.manifest, true
}

func (c *manifestCache) put(key string, m ObjectManifest) {
	if c == nil || c.ttl < 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	expires := c.now().Add(c.ttl)
	if element, ok := c.items[key]; ok {
		element.Value = &manifestEntry{key: key, manifest: m, expires: expires}
		c.order.MoveToFront(element)
		return
	}
	c.items[key] = c.order.PushFront(&manifestEntry{key: key, manifest: m, expires: expires})
	for c.order.Len() > c.max {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*manifestEntry).key)
	}
}

// manifest returns the decoded manifest for an authorized lesson row. A cache hit
// costs no object read; a miss costs one GET of the manifest object. Slide HTML and
// assets are not read here when the manifest carries their SHA-256 digests (every
// publisher writes them); only a legacy slide without digests is read once to derive
// them, and the result is cached. The returned value is shared: callers must not
// mutate it.
func (s *Service) manifest(ctx context.Context, key string, version int) (ObjectManifest, error) {
	cacheKey := manifestCacheKey(key, version)
	if m, ok := s.manifests.get(cacheKey); ok {
		return m, nil
	}
	raw, err := s.opts.Objects.Get(ctx, key)
	if err != nil || len(raw) > maxObjectBytes {
		return ObjectManifest{}, errInvalid
	}
	m, err := decodeManifest(raw, s.opts.AssetOrigin)
	if err != nil || m.Version != version {
		return ObjectManifest{}, errInvalid
	}
	slides := make([]ObjectSlide, len(m.Slides))
	copy(slides, m.Slides)
	for i, slide := range slides {
		if hasDigests(slide) {
			continue
		}
		html, _, hashes, err := s.document(ctx, slide)
		if err != nil {
			return ObjectManifest{}, errInvalid
		}
		slide.HTMLSHA256 = checksum(html)
		slide.AssetSHA256 = hashes
		slides[i] = slide
	}
	m.Slides = slides
	s.manifests.put(cacheKey, m)
	return m, nil
}

func hasDigests(slide ObjectSlide) bool {
	if slide.HTMLSHA256 == "" {
		return false
	}
	for _, ref := range slide.Assets {
		if slide.AssetSHA256[ref] == "" {
			return false
		}
	}
	return true
}
