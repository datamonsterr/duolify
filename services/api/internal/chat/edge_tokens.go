package chat

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	edgeServiceDomain = "duolify-edge-chat-v1."
	edgeChannelDomain = "duolify-chat-channel-v1."
	channelTTL        = 5 * time.Minute
)

var errEdgeCredential = errors.New("invalid edge chat credential")

type edgeScope struct {
	LearnerID string `json:"learner_id"`
	SessionID string `json:"session_id"`
	CourseID  string `json:"course_id"`
	ThreadID  string `json:"thread_id"`
	Origin    string `json:"origin"`
	Surface   string `json:"surface,omitempty"`
	ExpiresAt int64  `json:"expires_at"`
}

type edgeServiceClaims struct {
	Exp        int64      `json:"exp"`
	Method     string     `json:"method"`
	Path       string     `json:"path"`
	BodySHA256 string     `json:"body_sha256"`
	Transport  string     `json:"transport"`
	Scope      *edgeScope `json:"scope,omitempty"`
}

func strictEdgeJSON(raw []byte, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after edge chat JSON")
	}
	return nil
}

func edgeMAC(secret, domain, payload string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(mac, domain)
	_, _ = io.WriteString(mac, payload)
	return mac.Sum(nil)
}

func signEdge(secret, domain string, claims any) (string, error) {
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	sig := base64.RawURLEncoding.EncodeToString(edgeMAC(secret, domain, payload))
	return payload + "." + sig, nil
}

func verifyEdge(secret, domain, raw string, claims any) error {
	payload, sig, ok := strings.Cut(raw, ".")
	if !ok || len(payload) > 4096 || len(sig) != 43 {
		return errEdgeCredential
	}
	expected, err := base64.RawURLEncoding.Strict().DecodeString(sig)
	if err != nil || !hmac.Equal(expected, edgeMAC(secret, domain, payload)) {
		return errEdgeCredential
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(payload)
	if err != nil || strictEdgeJSON(decoded, claims) != nil {
		return errEdgeCredential
	}
	return nil
}

func bodyDigest(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func verifyEdgeService(secret string, r *http.Request, body []byte, now time.Time) (edgeServiceClaims, error) {
	var claims edgeServiceClaims
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || verifyEdge(secret, edgeServiceDomain, raw, &claims) != nil {
		return claims, errEdgeCredential
	}
	if claims.Exp <= now.Unix() || claims.Exp > now.Add(35*time.Second).Unix() ||
		claims.Method != r.Method || claims.Path != r.URL.EscapedPath() || r.URL.RawQuery != "" ||
		claims.BodySHA256 != bodyDigest(body) || (claims.Transport != "websocket" && claims.Transport != "sse") {
		return claims, errEdgeCredential
	}
	return claims, nil
}
