#!/bin/sh
# Entrypoint of the web/proxy image. Derives CLERK_FAPI_ORIGIN (the Clerk Frontend API origin the
# /sso-bridge page may load scripts from and call) from CLERK_PUBLISHABLE_KEY, which is already in
# the deploy .env: pk_(test|live)_<base64 of "<frontend-api-host>$">. No secret is involved. An
# explicit CLERK_FAPI_ORIGIN wins. Anything that does not decode to a host name leaves it empty,
# and the SSO page then simply has no Clerk origin (SSO off, as without the key).
set -eu
if [ -z "${CLERK_FAPI_ORIGIN:-}" ] && [ -n "${CLERK_PUBLISHABLE_KEY:-}" ]; then
  b64=${CLERK_PUBLISHABLE_KEY#pk_*_}
  while [ $((${#b64} % 4)) -ne 0 ]; do b64="$b64="; done
  host=$(printf '%s' "$b64" | base64 -d 2>/dev/null | tr -d '$\r\n' || true)
  case $host in
    '' | *[!A-Za-z0-9.-]*) ;;
    *) CLERK_FAPI_ORIGIN="https://$host"; export CLERK_FAPI_ORIGIN ;;
  esac
fi
exec "$@"
