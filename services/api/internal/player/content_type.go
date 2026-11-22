package player

import "strings"

// ImmutableCacheControl is set on every lesson object: keys are content-addressed or
// build-versioned and never rewritten (ADR 0022).
const ImmutableCacheControl = "public, max-age=31536000, immutable"

var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".json": "application/json",
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".webp": "image/webp",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".wav":  "audio/wav",
	".mp3":  "audio/mpeg",
	".ogg":  "audio/ogg",
	".webm": "audio/webm",
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
}

// ContentTypeForKey derives the stored media type from the object key extension. Public
// assets are served with nosniff, so an explicit type is required.
func ContentTypeForKey(key string) string {
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		if t, ok := contentTypes[strings.ToLower(key[i:])]; ok {
			return t
		}
	}
	return "application/octet-stream"
}
