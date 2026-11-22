# Staging test inbox

The flow suite signs in with an emailed code. On staging the code is mailed through Resend, so the suite reads it from a test inbox instead of a log.

```
API --Resend--> e2e-<flow>-<id>@test.duolify.online
  --MX (Cloudflare Email Routing, subdomain test.duolify.online)--> catch-all rule --> Worker duolify-e2e-mailbox
  --> KV (code list per address, 10 min TTL) <-- GET https://e2e-mailbox.duolify.online/codes?to=<address> (Bearer token)
```

- Code: `infra/e2e-mailbox/` (`worker.js`, `wrangler.jsonc`, `deploy.sh`, `code.sh`). The Worker only keeps six-digit codes, never message bodies.
- Cloudflare setup (done once with the owner API token): Email Routing enabled for the subdomain `test.duolify.online` (adds its MX and SPF records), a catch-all rule with action "send to Worker", a KV namespace `duolify-e2e-mailbox`, a custom domain `e2e-mailbox.duolify.online` for the Worker. No Cloudflare change touches `duolify.online` itself or Resend's `send.` records.
- Secret: `E2E_MAILBOX_TOKEN` in `~/.config/duolify/secrets.env` (also the Worker secret `MAILBOX_TOKEN`). It is a GitHub secret only if a workflow runs the suite against staging.
- Deploy or rotate: `bash infra/e2e-mailbox/deploy.sh`.
- Run the suite against staging:

```sh
cd apps/client
E2E_STACK_URL=https://staging.duolify.online E2E_LIVE=1 E2E_STACK_EMAIL_DOMAIN=test.duolify.online \
  E2E_STACK_CODE_COMMAND='bash ../../infra/e2e-mailbox/code.sh "$1"' pnpm exec playwright test --config playwright.stack.config.ts
```

Course flows spend real provider credits on staging (the learner's daily check-in funds them).
