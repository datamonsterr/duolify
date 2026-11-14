// Package player serves authorized, object-backed lesson content, never progress.
package player

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

const maxObjectBytes = 1 << 20

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
var networkURL = regexp.MustCompile(`(?i)(?:https?:)?//[^\s<>"'()\\]+`)
var errInvalid = errors.New("invalid lesson content")

// Checkpoint identifies a bounded moment in a slide.
type Checkpoint struct {
	ID   string `json:"id"`
	AtMS int    `json:"at_ms"`
}

// ObjectSlide is the closed storage schema for one slide.
type ObjectSlide struct {
	HTMLSHA256    string            `json:"html_sha256,omitempty"`
	AssetSHA256   map[string]string `json:"asset_sha256,omitempty"`
	ID            string            `json:"slide_id"`
	DurationMS    int               `json:"duration_ms"`
	HTMLKey       string            `json:"html_key"`
	Assets        []string          `json:"assets"`
	Checkpoints   []Checkpoint      `json:"checkpoints"`
	NarrationText string            `json:"narration_text"`
	Explanation   string            `json:"explanation,omitempty"`
}

// ObjectManifest contains ordered immutable document references.
type ObjectManifest struct {
	Version int           `json:"version"`
	Slides  []ObjectSlide `json:"slides"`
}

// NarrationAsset points at owner-only audio and alignment routes.
type NarrationAsset struct {
	AudioURL     string `json:"audio_url"`
	AlignmentURL string `json:"alignment_url"`
	DurationMS   int    `json:"duration_ms"`
	Characters   int    `json:"characters"`
}

// Slide is the public manifest projection, without internal object keys.
type Slide struct {
	HTMLSHA256      string            `json:"html_sha256"`
	AssetSHA256     map[string]string `json:"asset_sha256"`
	ID              string            `json:"slide_id"`
	DurationMS      int               `json:"duration_ms"`
	HTMLURL         string            `json:"html_url"`
	Assets          []string          `json:"assets"`
	Checkpoints     []Checkpoint      `json:"checkpoints"`
	NarrationStatus string            `json:"narration_status"`
	// NarrationReason is a fixed code for a disabled slide (for example insufficient_credits).
	NarrationReason string `json:"narration_reason,omitempty"`
	// Narration is present only when status is ready; it never exposes object keys.
	Narration     *NarrationAsset `json:"narration,omitempty"`
	NarrationText string          `json:"narration_text"`
	Explanation   string          `json:"explanation"`
	// Segments are plain-text dialogue for pinned speakers, validated at job time.
	Segments json.RawMessage `json:"segments,omitempty"`
}

// Manifest is the authorized lesson playback DTO.
type Manifest struct {
	LessonID    string  `json:"lesson_id"`
	CourseID    string  `json:"course_id"`
	Title       string  `json:"title"`
	Version     int     `json:"version"`
	AssetOrigin string  `json:"asset_origin"`
	Slides      []Slide `json:"slides"`
	// Cast is the immutable payload pinned when the generation job started.
	Cast json.RawMessage `json:"cast,omitempty"`
}

// ValidOrigin accepts a single exact HTTPS origin or local HTTP development origin.
func ValidOrigin(origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && (u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && !strings.ContainsAny(origin, "*'\"; <>\\\n\r\t")
}
func validKey(key string) bool {
	return strings.HasPrefix(key, "lessons/") && path.Clean(key) == key && !strings.ContainsAny(key, "\\?#\r\n") && len(key) < 500
}
func assetAllowed(value, origin string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme+"://"+u.Host == origin && u.User == nil && u.Fragment == "" && len(value) <= 2000
}

// ValidateManifest enforces stable identifiers, timing and asset boundaries.
func ValidateManifest(m ObjectManifest, origin string) error {
	if !ValidOrigin(origin) || m.Version < 1 || len(m.Slides) < 1 || len(m.Slides) > 100 {
		return errInvalid
	}
	seen := map[string]bool{}
	for _, s := range m.Slides {
		if !identifier.MatchString(s.ID) || seen[s.ID] || s.DurationMS < 1 || s.DurationMS > 120000 || !validKey(s.HTMLKey) || s.Assets == nil || len(s.Assets) > 100 || s.Checkpoints == nil || len(s.Checkpoints) > 100 || len(s.NarrationText) > 20000 || len(s.Explanation) > 20000 {
			return errInvalid
		}
		seen[s.ID] = true
		for _, a := range s.Assets {
			if !assetAllowed(a, origin) {
				return errInvalid
			}
		}
		checkpoints := map[string]bool{}
		last := -1
		for _, c := range s.Checkpoints {
			if !identifier.MatchString(c.ID) || checkpoints[c.ID] || c.AtMS < last || c.AtMS < 0 || c.AtMS > s.DurationMS {
				return errInvalid
			}
			checkpoints[c.ID] = true
			last = c.AtMS
		}
	}
	return nil
}
func decodeManifest(raw []byte, origin string) (ObjectManifest, error) {
	var m ObjectManifest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil {
		return m, errInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return m, errInvalid
	}
	return m, ValidateManifest(m, origin)
}

// ValidateHTML rejects declarative escape/resource vectors before publication and
// again on reads. Arbitrary script execution remains confined by host CSP/sandbox.
func ValidateHTML(document, origin string) error {
	if !ValidOrigin(origin) || len(document) == 0 || len(document) > maxObjectBytes {
		return errInvalid
	}
	root, err := html.Parse(strings.NewReader(document))
	if err != nil {
		return errInvalid
	}
	var walk func(*html.Node) error
	walk = func(n *html.Node) error {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "base", "iframe", "frame", "object", "embed", "form":
				return errInvalid
			}
			for _, a := range n.Attr {
				if a.Key == "srcdoc" || a.Key == "srcset" || (n.Data == "meta" && a.Key == "http-equiv" && strings.EqualFold(a.Val, "refresh")) {
					return errInvalid
				}
				switch a.Key {
				case "src", "href", "poster", "action", "formaction", "data":
					if !strings.HasPrefix(a.Val, "#") && !assetAllowed(a.Val, origin) {
						return errInvalid
					}
				}
			}
		}
		// Tokenized text/attributes include entity decoding, closing encoded URLs.
		values := []string{n.Data}
		for _, a := range n.Attr {
			values = append(values, a.Val)
		}
		for _, v := range values {
			for _, ref := range networkURL.FindAllString(v, -1) {
				if !assetAllowed(ref, origin) {
					return errInvalid
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}
