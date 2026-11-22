#!/usr/bin/env bash
# Tests where `selfhost.sh smoke` connects and how it waits for the proxy. Reproduces the staging failure:
# SITE_ADDRESS lists two names, the origin certificate comes from a private CA, and the proxy is published on
# BIND_ADDRESS (the VM's address), so nothing listens on 127.0.0.1. Needs openssl and curl; no Docker.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
tmp=$(mktemp -d)
pids=()
cleanup() {
  local p
  for p in ${pids[@]+"${pids[@]}"}; do kill "$p" 2>/dev/null || true; done
  rm -rf "$tmp"
}
trap cleanup EXIT
failures=0
pass() { printf 'ok   %s\n' "$1"; }
bad() { printf 'FAIL %s\n' "$1"; failures=$((failures + 1)); }

mkdir -p "$tmp/deploy/site.d"
cp "$here/selfhost.sh" "$tmp/deploy/"
ca=$tmp/deploy/site.d/origin-ca.pem
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -subj /CN=test-ca -keyout "$tmp/ca.key" -out "$ca" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -subj /CN=staging.test -keyout "$tmp/leaf.key" -out "$tmp/leaf.csr" >/dev/null 2>&1
printf 'subjectAltName=DNS:staging.test,DNS:staging-api.test\n' >"$tmp/san"
openssl x509 -req -in "$tmp/leaf.csr" -CA "$ca" -CAkey "$tmp/ca.key" -CAcreateserial -days 2 -extfile "$tmp/san" -out "$tmp/leaf.pem" >/dev/null 2>&1

port=$((20000 + RANDOM % 20000))
env_file=$tmp/deploy/.env
selfhost() { SELFHOST_ENV_FILE=$env_file bash "$tmp/deploy/selfhost.sh" "$@"; }
target() { printf '%s\n' "$@" >"$env_file"; selfhost smoke-target; }
origin() { # ADDRESS: an HTTPS origin on ADDRESS:$port with the private-CA certificate, after an optional delay
  (sleep "${2:-0}" && exec openssl s_server -accept "$1:$port" -cert "$tmp/leaf.pem" -key "$tmp/leaf.key" -www >/dev/null 2>&1) &
  pids+=($!)
}

# 1. The staging settings: two site addresses, proxy bound to a non-loopback address.
out=$(target 'SITE_ADDRESS=staging.test, staging-api.test' BIND_ADDRESS=127.0.0.2 "HTTPS_PORT=$port" HTTP_PORT=80)
if grep -q "^https://staging.test:$port\$" <<<"$out" && grep -q -- "^staging.test:$port:127.0.0.2\$" <<<"$out" && grep -q -- origin-ca.pem <<<"$out"; then
  pass "connects to BIND_ADDRESS with the first site as SNI/Host and the origin CA"
else bad "target for a bound proxy: $out"; fi

# 2. A wildcard or empty BIND_ADDRESS means loopback.
for b in 0.0.0.0 ''; do
  out=$(target 'SITE_ADDRESS=staging.test' "BIND_ADDRESS=$b" "HTTPS_PORT=$port")
  if grep -q -- "^staging.test:$port:127.0.0.1\$" <<<"$out"; then pass "BIND_ADDRESS='$b' connects to loopback"; else bad "BIND_ADDRESS='$b': $out"; fi
done

# 3. Plain HTTP sites use the bound address and HTTP_PORT.
out=$(target 'SITE_ADDRESS=:80' BIND_ADDRESS=10.1.2.3 HTTP_PORT=8088)
if [[ $out == http://10.1.2.3:8088 ]]; then pass "plain HTTP uses BIND_ADDRESS and HTTP_PORT"; else bad "http target: $out"; fi

# 4. The origin starts 3 s after the wait begins (the proxy container is healthy before Caddy binds):
#    the wait retries and then passes, trusting the private-CA certificate.
printf '%s\n' 'SITE_ADDRESS=staging.test, staging-api.test' BIND_ADDRESS=127.0.0.2 "HTTPS_PORT=$port" >"$env_file"
origin 127.0.0.2 3
if SMOKE_WAIT_SECONDS=20 selfhost smoke-wait; then pass "waits for a proxy that listens late (private-CA certificate trusted)"
else bad "smoke-wait did not reach the late origin"; fi

# 5. Both names verify against the same certificate on that listener.
if curl -fsS -o /dev/null --resolve "staging-api.test:$port:127.0.0.2" --cacert "$ca" "https://staging-api.test:$port/"; then pass "second site name verified by the same certificate"
else bad "second site name"; fi

# 6. The wait is bounded and shows the curl error when nothing listens.
printf '%s\n' 'SITE_ADDRESS=staging.test' BIND_ADDRESS=127.0.0.3 "HTTPS_PORT=$port" >"$env_file"
start=$SECONDS
if err=$(SMOKE_WAIT_SECONDS=2 selfhost smoke-wait 2>&1); then bad "wait passed with nothing listening"
elif ((SECONDS - start <= 8)) && [[ $err == *curl* ]]; then pass "wait is bounded and shows the curl error"
else bad "unbounded or silent wait: $err"; fi

((failures == 0)) || { printf '%s failure(s)\n' "$failures" >&2; exit 1; }
