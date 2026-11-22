#!/usr/bin/env bash
# Asserts the security headers of one origin (web client paths and the API) as served right now.
# Usage: check-security-headers.sh BASE_URL [ASSET_ORIGIN] [-- curl options...]
# Prints one `FAIL ...` line per problem and exits 1; exits 0 when every header is right.
# Used by `selfhost.sh smoke` (after a deploy, against the proxy) and test-security-headers.sh.
set -uo pipefail
base=${1:?usage: check-security-headers.sh BASE_URL [ASSET_ORIGIN] [-- curl options...]}
shift
asset=''
[[ ${1:-} == -- || $# -eq 0 ]] || { asset=$1; shift; }
[[ ${1:-} == -- ]] && shift
curl_opts=("$@")
failures=0
fail() { printf 'FAIL %s\n' "$1"; failures=$((failures + 1)); }

headers() { curl -sS -D - -o /dev/null "${curl_opts[@]}" "$@" | tr -d '\r'; }
# values NAME < headers: every value of header NAME (case-insensitive), one per line.
values() { awk -v n="$1" 'BEGIN{n=tolower(n)} { i=index($0,":"); if (i>0 && tolower(substr($0,1,i-1))==n) { v=substr($0,i+1); sub(/^[ \t]+/,"",v); print v } }'; }

# expect LABEL NAME VALUE HEADERS: NAME is present exactly once and equals VALUE.
expect() {
  local got count
  got=$(values "$2" <<<"$4")
  count=$(grep -c . <<<"$got" || true)
  if [[ $count != 1 ]]; then fail "$1: $2 sent $count times (want once)"
  elif [[ $got != "$3" ]]; then fail "$1: $2 is '$got', want '$3'"; fi
}
# csp_has LABEL HEADERS TOKEN...: the single CSP contains each token as a whole directive fragment.
csp_has() {
  local label=$1 hdrs=$2 csp tok
  shift 2
  csp=$(values Content-Security-Policy <<<"$hdrs")
  if [[ $(grep -c . <<<"$csp" || true) != 1 ]]; then fail "$label: Content-Security-Policy sent $(grep -c . <<<"$csp" || true) times (want once)"; return; fi
  for tok in "$@"; do
    [[ "; $csp;" == *"; $tok;"* || "; $csp;" == *"; $tok "* || "; $csp;" == *" $tok;"* || "; $csp;" == *" $tok "* ]] || fail "$label: CSP lacks '$tok'"
  done
}
csp_lacks() { # LABEL HEADERS TOKEN...
  local label=$1 csp tok
  csp=$(values Content-Security-Policy <<<"$2")
  shift 2
  for tok in "$@"; do [[ $csp != *"$tok"* ]] || fail "$label: CSP must not contain '$tok'"; done
}
directive() { # NAME CSP: the sources of one directive
  tr ';' '\n' <<<"$2" | sed 's/^ *//' | awk -v d="$1" '$1==d {print}'
}

hsts='max-age=31536000; includeSubDomains'

# --- web client: the SPA entry and a deep link ---
for path in / /courses/headers-check; do
  h=$(headers "$base$path")
  expect "web $path" Strict-Transport-Security "$hsts" "$h"
  expect "web $path" X-Content-Type-Options nosniff "$h"
  expect "web $path" Referrer-Policy strict-origin-when-cross-origin "$h"
  pp=$(values Permissions-Policy <<<"$h")
  [[ $pp == *'microphone=(self)'* && $pp == *'camera=()'* && $pp == *'geolocation=()'* ]] || fail "web $path: Permissions-Policy is '$pp'"
  csp_has "web $path" "$h" "default-src 'self'" "frame-ancestors 'none'" "base-uri 'self'" "object-src 'none'" "form-action 'self'" "frame-src 'self'"
  csp_lacks "web $path" "$h" unsafe-eval "script-src 'self' 'unsafe-inline'"
  csp=$(values Content-Security-Policy <<<"$h")
  [[ $(directive script-src "$csp") != *unsafe-inline* ]] || fail "web $path: script-src allows unsafe-inline"
  if [[ -n $asset ]]; then
    for d in connect-src img-src media-src; do [[ $(directive $d "$csp") == *" $asset"* ]] || fail "web $path: $d lacks the asset origin $asset"; done
  fi
done

# --- the Clerk page keeps the web policy and adds Clerk to script/connect/frame only ---
h=$(headers "$base/sso-bridge")
expect "web /sso-bridge" Strict-Transport-Security "$hsts" "$h"
csp_has "web /sso-bridge" "$h" "frame-ancestors 'none'" "object-src 'none'" "https://challenges.cloudflare.com" "https://img.clerk.com"
csp_lacks "web /sso-bridge" "$h" unsafe-eval

# --- the lesson frame: inline allowed, network limited to the asset origin, embeddable by the app only ---
h=$(headers "$base/lesson-frame.html")
expect "lesson frame" Strict-Transport-Security "$hsts" "$h"
expect "lesson frame" X-Content-Type-Options nosniff "$h"
expect "lesson frame" Referrer-Policy no-referrer "$h"
csp_has "lesson frame" "$h" "default-src 'none'" "frame-ancestors 'self'" "frame-src 'none'" "object-src 'none'" "form-action 'none'" "worker-src 'none'" "base-uri 'none'"
csp_lacks "lesson frame" "$h" unsafe-eval
csp=$(values Content-Security-Policy <<<"$h")
[[ $(directive connect-src "$csp") != *"'self'"* ]] || fail "lesson frame: connect-src allows 'self'"
[[ -z $asset ]] || [[ $(directive connect-src "$csp") == *" $asset"* ]] || fail "lesson frame: connect-src lacks the asset origin $asset"

# --- API: JSON only, nothing embeddable ---
h=$(headers "$base/v1/features")
expect "api" Strict-Transport-Security "$hsts" "$h"
expect "api" X-Content-Type-Options nosniff "$h"
expect "api" Referrer-Policy no-referrer "$h"
expect "api" Content-Security-Policy "default-src 'none'; frame-ancestors 'none'" "$h"
[[ $(values Cross-Origin-Resource-Policy <<<"$h" | grep -c .) == 1 ]] || fail "api: Cross-Origin-Resource-Policy missing or duplicated"

((failures == 0)) || exit 1
printf 'security headers ok on %s\n' "$base"
