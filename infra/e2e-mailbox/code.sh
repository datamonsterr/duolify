#!/usr/bin/env bash
# Print the sign-in codes mailed to <address> at the staging test inbox, oldest first.
# Use as E2E_STACK_CODE_COMMAND='bash infra/e2e-mailbox/code.sh "$1"' with E2E_STACK_EMAIL_DOMAIN=test.duolify.online.
# E2E_MAILBOX_TOKEN comes from the environment or ~/.config/duolify/secrets.env; E2E_MAILBOX_URL overrides the host.
set -euo pipefail
secrets=${DUOLIFY_SECRETS:-$HOME/.config/duolify/secrets.env}
token=${E2E_MAILBOX_TOKEN:-$(sed -n 's/^E2E_MAILBOX_TOKEN=//p' "$secrets" 2>/dev/null | tail -n 1)}
[[ -n $token && -n ${1:-} ]] || { echo "usage: E2E_MAILBOX_TOKEN=... $0 <address>" >&2; exit 2; }
curl -fsS -H "Authorization: Bearer $token" --get --data-urlencode "to=$1" "${E2E_MAILBOX_URL:-https://e2e-mailbox.duolify.online}/codes"
