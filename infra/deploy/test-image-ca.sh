#!/usr/bin/env bash
# Regression guard for "the image has no TLS root certificates": every image that makes outbound HTTPS calls
# (R2, Clerk, PostHog, the engine's providers) must carry a usable CA bundle at /etc/ssl/certs/ca-certificates.crt,
# the first path Go, OpenSSL and curl read on Debian-family images.
#   test-image-ca.sh <image> [<image> ...]
# Offline: the bundle is copied out with `docker create` + `docker cp` (distroless images have no shell), so it also
# works for images that cannot `exec`. For each image it asserts the file exists, is not a stub (>= 100 kB and >= 100
# PEM certificates) and parses as X.509 with the ISRG Root X1 anchor present. It does NOT prove a live handshake;
# CI and the self-hosted stack talk plain HTTP to a local object store, which is how the missing bundle went unseen.
set -euo pipefail
(($# > 0)) || { echo "usage: $0 <image>..." >&2; exit 2; }
tmp=$(mktemp -d)
cid=''
cleanup() { [[ -z $cid ]] || docker rm -f "$cid" >/dev/null 2>&1 || true; rm -rf "$tmp"; }
trap cleanup EXIT
failures=0
fail() { printf 'FAIL %s: %s\n' "$1" "$2"; failures=$((failures + 1)); }
for image in "$@"; do
  if ! cid=$(docker create "$image" 2>/dev/null); then fail "$image" "cannot create a container (image missing?)"; cid=''; continue; fi
  bundle=$tmp/bundle.crt
  rm -f "$bundle"
  if ! docker cp "$cid:/etc/ssl/certs/ca-certificates.crt" "$bundle" >/dev/null 2>&1; then
    fail "$image" "/etc/ssl/certs/ca-certificates.crt does not exist"
  else
    size=$(wc -c <"$bundle")
    count=$(grep -c 'BEGIN CERTIFICATE' "$bundle" || true)
    subjects=$(openssl crl2pkcs7 -nocrl -certfile "$bundle" 2>/dev/null | openssl pkcs7 -print_certs -noout 2>/dev/null || true)
    if ((size < 100000 || count < 100)); then fail "$image" "CA bundle is a stub ($size bytes, $count certificates)"
    elif ! grep -q 'ISRG Root X1' <<<"$subjects"; then fail "$image" "CA bundle does not parse or lacks ISRG Root X1"
    else printf 'ok   %s: CA bundle %s bytes, %s certificates, ISRG Root X1 present\n' "$image" "$size" "$count"; fi
  fi
  docker rm -f "$cid" >/dev/null 2>&1 || true
  cid=''
done
((failures == 0)) || { printf '%s image(s) without a usable CA bundle\n' "$failures" >&2; exit 1; }
