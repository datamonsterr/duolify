package accounts

import (
	"net/http"
	"strings"
	"time"
)

// Web session mode (openspec/changes/persist-web-session, ADR 0004 amendment).
//
// A browser client declares itself with X-Duolify-Client: web. Its refresh credential then
// travels only in an HttpOnly cookie and never in a JSON body, so script (and XSS) cannot read
// it, while a reload can still mint a fresh in-memory access token. Native (Tauri) clients send
// no such header and keep the body/keychain flow: the server never sets the cookie for them
// and never reads it for them, so neither mode can downgrade the other.
const (
	// ClientHeader names the request header a client uses to declare its mode.
	ClientHeader = "X-Duolify-Client"
	// ClientWeb is the only value that enables cookie mode.
	ClientWeb = "web"
	// RefreshCookieName is the web refresh credential cookie.
	RefreshCookieName = "duolify_refresh"
	// refreshCookiePath covers /v1/auth/refresh and /v1/auth/logout, the two endpoints that
	// read it. A cookie has one Path and the two endpoints are siblings, so this is the
	// narrowest prefix that reaches both.
	refreshCookiePath = "/v1/auth/"
)

// WebTokens is the body returned to cookie-mode clients: no refresh credential.
type WebTokens struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// ConfigureWebOrigins lists the exact browser origins allowed to use cookie mode
// (normally API_ALLOWED_ORIGINS, which is the public web origin). With none configured
// every cookie-mode request is refused.
func (s *Service) ConfigureWebOrigins(origins []string) {
	s.webOrigins = nil
	for _, origin := range origins {
		if origin = strings.TrimSpace(origin); origin != "" {
			s.webOrigins = append(s.webOrigins, origin)
		}
	}
}

// WebOriginsFromEnv reads the same origin list the CORS layer trusts.
func WebOriginsFromEnv(getenv func(string) string) []string {
	return strings.Split(getenv("API_ALLOWED_ORIGINS"), ",")
}

func isWebClient(r *http.Request) bool {
	return r.Header.Get(ClientHeader) == ClientWeb
}

// webGate decides the mode of a cookie-relevant request. It returns (web, ok): ok is false
// after a 403 has been written. Cookie mode requires the declared header, a same-origin
// Fetch Metadata value when the browser sends one, and an exact configured Origin, so a
// cross-site page, a form post or a sibling subdomain cannot ride the cookie.
func (s *Service) webGate(w http.ResponseWriter, r *http.Request) (web bool, ok bool) {
	if !isWebClient(r) {
		return false, true
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return true, deny(w)
	}
	origin := r.Header.Get("Origin")
	for _, allowed := range s.webOrigins {
		if origin != "" && origin == allowed {
			return true, true
		}
	}
	return true, deny(w)
}

func deny(w http.ResponseWriter) bool {
	respond(w, http.StatusForbidden, map[string]string{"error": "Request origin is not allowed."})
	return false
}

// refreshCookie builds the cookie. Max-Age is the remaining life of the session family, which
// is the refresh lifetime; there is no Domain attribute (host-only) and no Expires.
func refreshCookie(value string, expiry time.Time) *http.Cookie {
	age := int(time.Until(expiry).Seconds())
	if age < 1 {
		age = 1
	}
	return &http.Cookie{Name: RefreshCookieName, Value: value, Path: refreshCookiePath, MaxAge: age, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode}
}

func clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: RefreshCookieName, Value: "", Path: refreshCookiePath, MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
}

// clearWebCookie clears the cookie on bearer-authenticated revocations, only for web clients.
func clearWebCookie(w http.ResponseWriter, r *http.Request) {
	if isWebClient(r) {
		clearRefreshCookie(w)
	}
}

// respondTokens sends credentials in the caller's mode.
func respondTokens(w http.ResponseWriter, web bool, tokens Tokens, expiry time.Time) {
	if !web {
		respond(w, http.StatusOK, tokens)
		return
	}
	http.SetCookie(w, refreshCookie(tokens.RefreshToken, expiry))
	respond(w, http.StatusOK, WebTokens{AccessToken: tokens.AccessToken, ExpiresIn: tokens.ExpiresIn})
}

// cookieRefreshToken returns the cookie value, if any.
func cookieRefreshToken(r *http.Request) string {
	if c, err := r.Cookie(RefreshCookieName); err == nil {
		return c.Value
	}
	return ""
}
