#!/usr/bin/env bash
# Tests the edge security headers against a real Caddy (the image the stack uses) with the repository's
# Caddyfile and security-headers.caddy, a stand-in for the API that sends the API's headers, and the
# checker `selfhost.sh smoke` runs after a deploy. Also tests the Clerk-origin derivation of the proxy
# entrypoint and that the checker catches a proxy without the headers. Needs docker, curl, python3.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
tmp=$(mktemp -d)
pids=()
name=sec-headers-test-$$
cleanup() {
  local p
  for p in ${pids[@]+"${pids[@]}"}; do kill "$p" 2>/dev/null || true; done
  docker rm -f "$name" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT
failures=0
pass() { printf 'ok   %s\n' "$1"; }
bad() { printf 'FAIL %s\n' "$1"; failures=$((failures + 1)); }

# 1. Clerk Frontend API origin from the publishable key.
key_for() { printf '%s$' "$1" | base64 | tr -d '=\n'; }
# shellcheck disable=SC2016 # the ${...} is for the inner sh
derive() { env -i PATH="$PATH" "$@" sh "$here/proxy-entrypoint.sh" sh -c 'printf %s "${CLERK_FAPI_ORIGIN:-}"'; }
if [[ $(derive "CLERK_PUBLISHABLE_KEY=pk_test_$(key_for clerk.duolify.online)") == https://clerk.duolify.online ]]; then pass "origin from a pk_test key"; else bad "pk_test derivation"; fi
if [[ $(derive "CLERK_PUBLISHABLE_KEY=pk_live_$(key_for happy-cat-12.clerk.accounts.dev)") == https://happy-cat-12.clerk.accounts.dev ]]; then pass "origin from a pk_live key (padding restored)"; else bad "pk_live derivation"; fi
if [[ -z $(derive CLERK_PUBLISHABLE_KEY=pk_test_not-base64!) && -z $(derive CLERK_PUBLISHABLE_KEY=) && -z $(derive A=1) ]]; then pass "garbage or no key leaves the origin empty"; else bad "garbage key"; fi
if [[ $(derive CLERK_FAPI_ORIGIN=https://x.example "CLERK_PUBLISHABLE_KEY=pk_test_$(key_for clerk.other.example)") == https://x.example ]]; then pass "explicit CLERK_FAPI_ORIGIN wins"; else bad "explicit origin"; fi

# 2. Real Caddy: static files, a stand-in API, the repository config.
mkdir -p "$tmp/srv/assets" "$tmp/site.d"
echo '<!doctype html><div id="root"></div>' >"$tmp/srv/index.html"
echo '<!doctype html><title>frame</title>' >"$tmp/srv/lesson-frame.html"
echo 'console.log(1)' >"$tmp/srv/assets/app.js"
api_port=$((20000 + RANDOM % 10000))
web_port=$((30000 + RANDOM % 10000))
cat >"$tmp/api.py" <<PY
import http.server
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        for k, v in [("Strict-Transport-Security", "max-age=31536000; includeSubDomains"), ("X-Content-Type-Options", "nosniff"),
                     ("Referrer-Policy", "no-referrer"), ("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'"),
                     ("Cross-Origin-Resource-Policy", "same-site"), ("Content-Type", "application/json")]:
            self.send_header(k, v)
        self.end_headers(); self.wfile.write(b"{}")
    def log_message(self, *a): pass
http.server.HTTPServer(("127.0.0.1", $api_port), H).serve_forever()
PY
python3 -I "$tmp/api.py" &
pids+=($!)
asset=https://assets.test.example
docker run -d --name "$name" --network host \
  -e "SITE_ADDRESS=http://127.0.0.1:$web_port" -e "API_UPSTREAM=127.0.0.1:$api_port" -e "LESSON_ASSET_ORIGIN=$asset" \
  -e CLERK_FAPI_ORIGIN=https://clerk.test.example \
  -v "$here/Caddyfile:/etc/caddy/Caddyfile:ro" -v "$here/security-headers.caddy:/etc/caddy/security-headers.caddy:ro" \
  -v "$tmp/site.d:/etc/caddy/site.d:ro" -v "$tmp/srv:/srv:ro" caddy:2.11-alpine >/dev/null
for _ in $(seq 40); do curl -fsS -o /dev/null "http://127.0.0.1:$web_port/" 2>/dev/null && break; sleep 0.5; done

if out=$(bash "$here/check-security-headers.sh" "http://127.0.0.1:$web_port" "$asset" 2>&1); then pass "Caddy sends the web, SSO, lesson-frame and API headers exactly once"
else bad "headers on Caddy: $out"; fi

# Spot checks the checker itself could share a blind spot on.
csp=$(curl -fsS -D - -o /dev/null "http://127.0.0.1:$web_port/sso-bridge" | tr -d '\r' | sed -n 's/^[Cc]ontent-[Ss]ecurity-[Pp]olicy: //p')
if [[ $csp == *"script-src 'self' https://clerk.test.example https://challenges.cloudflare.com"* ]]; then pass "/sso-bridge allows the derived Clerk origin"; else bad "sso csp: $csp"; fi
csp=$(curl -fsS -D - -o /dev/null "http://127.0.0.1:$web_port/" | tr -d '\r' | sed -n 's/^[Cc]ontent-[Ss]ecurity-[Pp]olicy: //p')
if [[ $csp != *clerk* && $csp == *"connect-src 'self'"*"$asset"*"https://us.i.posthog.com"* ]]; then pass "app pages have no Clerk origin; connect-src has asset and PostHog"; else bad "web csp: $csp"; fi
if [[ $csp == *"https://us-assets.i.posthog.com"* ]]; then pass "connect-src allows the PostHog remote-config host (staging console error)"; else bad "no PostHog assets host: $csp"; fi
code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Origin: https://x.example' "http://127.0.0.1:$web_port/v1/features")
if [[ $code == 200 ]]; then pass "/v1 still reaches the API through Caddy"; else bad "/v1 status $code"; fi

# 3. The checker fails on a proxy that sends nothing.
python3 -I -m http.server "$((web_port + 1))" --bind 127.0.0.1 --directory "$tmp/srv" >/dev/null 2>&1 &
pids+=($!)
sleep 1
if bash "$here/check-security-headers.sh" "http://127.0.0.1:$((web_port + 1))" "$asset" >/dev/null 2>&1; then bad "checker accepted a server without headers"; else pass "checker rejects a server without headers"; fi

((failures == 0)) || { printf '%s failure(s)\n' "$failures" >&2; exit 1; }
