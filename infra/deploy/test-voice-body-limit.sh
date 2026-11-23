#!/usr/bin/env bash
# Voice input: the edge refuses a body over the recording limit (1 MB audio plus framing) on
# POST /v1/voice/transcriptions before the API sees it, and leaves every other route alone. Runs the
# real Caddy image with the repository Caddyfile in front of a stand-in API. Needs docker, curl, python3.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
tmp=$(mktemp -d)
name=voice-limit-test-$$
port=$((20000 + RANDOM % 10000))
upstream=$((port + 1))
stub=""
cleanup() {
  [[ -z $stub ]] || kill "$stub" 2>/dev/null || true
  docker rm -f "$name" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT
failures=0
pass() { printf 'ok   %s\n' "$1"; }
bad() { printf 'FAIL %s\n' "$1"; failures=$((failures + 1)); }

cat >"$tmp/stub.py" <<PY
import http.server
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", 0)))
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok")
    def log_message(self, *args):
        pass
http.server.HTTPServer(("127.0.0.1", $upstream), H).serve_forever()
PY
python3 "$tmp/stub.py" &
stub=$!
mkdir -p "$tmp/site.d"
docker run -d --rm --name "$name" --network host -e "SITE_ADDRESS=:$port" -e "API_UPSTREAM=127.0.0.1:$upstream" \
  -v "$here/Caddyfile:/etc/caddy/Caddyfile:ro" -v "$here/security-headers.caddy:/etc/caddy/security-headers.caddy:ro" \
  -v "$tmp/site.d:/etc/caddy/site.d:ro" caddy:2.11-alpine >/dev/null
for _ in $(seq 1 30); do curl -s -o /dev/null "http://127.0.0.1:$port/v1/x" && break || sleep 0.5; done

head -c 1000000 /dev/zero >"$tmp/at-limit.bin"
head -c 1200000 /dev/zero >"$tmp/over.bin"
status() { curl -s -o /dev/null -w '%{http_code}' -X POST --data-binary "@$2" -H 'Content-Type: application/octet-stream' "http://127.0.0.1:$port$1"; }
[[ $(status /v1/voice/transcriptions "$tmp/at-limit.bin") == 200 ]] && pass "a 1 MB recording passes" || bad "1 MB recording"
[[ $(status /v1/voice/transcriptions "$tmp/over.bin") == 413 ]] && pass "a 1.2 MB body is refused with 413 at the edge" || bad "over-limit body"
[[ $(status /v1/notes "$tmp/over.bin") == 200 ]] && pass "other routes keep their own limits" || bad "other routes"
[[ $failures -eq 0 ]]
