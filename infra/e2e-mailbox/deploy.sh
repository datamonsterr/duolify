#!/usr/bin/env bash
# Deploy the staging test inbox Worker (owner workstation). Reads CLOUDFLARE_API_TOKEN, CLOUDFLARE_ACCOUNT_ID
# and E2E_MAILBOX_TOKEN from ~/.config/duolify/secrets.env without printing them; sets the Worker secret.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
secrets=${DUOLIFY_SECRETS:-$HOME/.config/duolify/secrets.env}
get() { sed -n "s/^$1=//p" "$secrets" | tail -n 1; }
export CLOUDFLARE_API_TOKEN CLOUDFLARE_ACCOUNT_ID
CLOUDFLARE_API_TOKEN=$(get CLOUDFLARE_API_TOKEN)
CLOUDFLARE_ACCOUNT_ID=$(get CLOUDFLARE_ACCOUNT_ID)
token=$(get E2E_MAILBOX_TOKEN)
[[ -n $token ]] || { echo "E2E_MAILBOX_TOKEN missing in $secrets" >&2; exit 1; }
cd "$here"
npx --yes wrangler@4 deploy
printf '%s' "$token" | npx wrangler@4 secret put MAILBOX_TOKEN
