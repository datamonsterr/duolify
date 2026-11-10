package player

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
)

func checksum(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func assetKey(ref, origin string) (string, error) {
	u, err := url.Parse(ref)
	if err != nil || !assetAllowed(ref, origin) || u.RawQuery != "" || u.RawPath != "" || !validKey(strings.TrimPrefix(u.Path, "/")) {
		return "", errInvalid
	}
	return strings.TrimPrefix(u.Path, "/"), nil
}

// AssetBytes is an authorized, bounded asset payload. Bodies are never fetched by
// caller-controlled URL: only validated keys under this lesson's immutable manifest.
type AssetBytes struct {
	URL  string `json:"url"`
	MIME string `json:"mime"`
	Data []byte `json:"data"`
}

func (s *Service) document(ctx context.Context, slide ObjectSlide) ([]byte, []AssetBytes, map[string]string, error) {
	raw, err := s.opts.Objects.Get(ctx, slide.HTMLKey)
	if err != nil || ValidateHTML(string(raw), s.opts.AssetOrigin) != nil || (slide.HTMLSHA256 != "" && checksum(raw) != slide.HTMLSHA256) {
		return nil, nil, nil, errInvalid
	}
	assets := make([]AssetBytes, 0, len(slide.Assets))
	hashes := map[string]string{}
	total := len(raw)
	for _, ref := range slide.Assets {
		key, err := assetKey(ref, s.opts.AssetOrigin)
		if err != nil {
			return nil, nil, nil, errInvalid
		}
		body, err := s.opts.Objects.Get(ctx, key)
		total += len(body)
		if err != nil || len(body) == 0 || total > 8<<20 {
			return nil, nil, nil, errInvalid
		}
		sum := checksum(body)
		if expected := slide.AssetSHA256[ref]; expected != "" && expected != sum {
			return nil, nil, nil, errInvalid
		}
		hashes[ref] = sum
		mime := http.DetectContentType(body)
		// Text assets require explicit types; never sniff untrusted content as app HTML.
		switch {
		case strings.HasSuffix(key, ".css"):
			mime = "text/css"
		case strings.HasSuffix(key, ".js"):
			mime = "text/javascript"
		case strings.HasSuffix(key, ".svg"):
			mime = "image/svg+xml"
		}
		assets = append(assets, AssetBytes{URL: ref, MIME: mime, Data: body})
	}
	return raw, assets, hashes, nil
}
