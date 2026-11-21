# Owner needs

Everything the project needs from the owner: decisions, credentials, accounts, approvals and manual checks. Kept up to date by the master session. Last updated 2026-11-21 (repo at `cd0178c`).

How to answer: reply in the master session with the item id and your choice (for example `D1: a`). Secrets never go in chat or git: put them in the place named in the item.

Facts behind several items, checked on 2026-11-21:

- The GitHub repo has no secrets and no variables. Only the `dev` environment exists, and it is empty. `staging` and `production` do not exist.
- `main` has no branch protection.
- The Railway project `duolify` has `prod` and `dev` environments with no services, so there is no Railway database and no production data.
- The self-hosted Docker stack (`duolify-selfhost-*`, draft PR #255) runs on this machine with mock providers.

## P0 — answers change what agents build now

### X1. CI runners are not picking up jobs

Since 2026-11-21 08:04 UTC every `pr-draft` run (PRs #255, #259, #260, #261) has stayed queued with no job started, for an hour and counting as of 09:04 UTC (PRs #262 and #263 are affected too). The jobs wait for the Namespace runner profiles `namespace-profile-datamonster01` and `namespace-profile-linux-xl`. The last runs that worked were on 2026-11-16. Agents cannot fix this: it needs the Namespace account.

Please check in the Namespace dashboard: is the GitHub integration still connected to `datamonsterr/duolify`, do both runner profiles still exist, and is the plan or its credit used up? Until this works no PR can show a green `ci-ok`; agents verify with the local mise tasks only.

### D1. Is there anything on Railway to migrate? ([#231](https://github.com/datamonsterr/duolify/issues/231), [#232](https://github.com/datamonsterr/duolify/issues/232))

E18 is written as a migration from a live Railway stack to Hetzner Helsinki, but Railway has no services and no data. Several E18 tickets only make sense with a live source.

- **a. Deploy fresh on Hetzner, skip Railway (recommended).** Drops the switchover gate (#240), the object copy tool and read-fallback (#237), the Railway rehearsal in #241, most of #243, the Railway facts in #232 and the Railway origin in #235. Keeps #233 (load baseline only), #234, #236, #239 and the self-hosted stack. I would rewrite the remaining tickets and ADR 0014 to match.
- **b. Keep the plan as written.** Deploy to Railway first (needs I1), then migrate with plan A or plan B.

Update 2026-11-21: on your instruction to work through every agent-ready ticket, #240 and #237 are now being built as well. If you choose option a, their PRs are closed unmerged; the rest is unaffected.

### D2. What happens to the `deploy-dev` workflow? ([#54](https://github.com/datamonsterr/duolify/issues/54))

It fails on every green push to `main` because `RAILWAY_TOKEN` is not set in the `dev` environment.

- **a. Disable it until the Hetzner deploy job from #239 exists (recommended if D1 = a).** I can open that PR.
- **b. Keep it:** set `RAILWAY_TOKEN` and `POSTHOG_API_KEY` in GitHub environment `dev`, and create the Railway services.

### C1. Golden dataset and evaluation secrets ([#25](https://github.com/datamonsterr/duolify/issues/25))

This unblocks #109, the only agent ticket outside E18, and through it #116, #145 and #147.

1. Create the Hugging Face dataset `datamonsterr/duolify-golden` (private) and a token with write access. I can prepare the initial revision from `evals/fixtures` for you to review and publish.
2. Set GitHub repository secrets: `HF_TOKEN`, `ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`. `ELEVENLABS_API_KEY` can come later with C4.
3. Decide the live-evaluation spend limit. The issue proposes no amount. Suggested: 5 USD per CI run and 50 USD per month.
4. Say who may publish feature cases to the dataset. Default: only you.

## P1 — needed for a deployed environment and live providers

### A1. Protect `main` ([#54](https://github.com/datamonsterr/duolify/issues/54))

Approve a branch protection rule on `main` that requires `checks / ci-ok`. I can apply it with `gh` once you say yes. No cost.

### I1. Hosting accounts and staging ([#232](https://github.com/datamonsterr/duolify/issues/232), [#238](https://github.com/datamonsterr/duolify/issues/238), [#235](https://github.com/datamonsterr/duolify/issues/235))

1. Create a Hetzner Cloud account with a project in `hel1`, and a Cloudflare account. Record whether the cost-optimized CX plans can be ordered in `hel1`.
2. **Decision D3: backup location, Falkenstein or Nuremberg?** #251 allowed either. Suggested: Falkenstein.
3. **Decision D4: which domain** serves the app, the API and public share links? This is also the open question in #37 (`SHARING_PUBLIC_DOMAIN`) and is needed for the Cloudflare zone.
4. Provision staging in `hel1` (#238): app and database VMs on a private network, firewall limited to Cloudflare ranges, SSH by key, a versioned `lesson-assets` bucket in `hel1`, and backup and DR-copy buckets in the D3 location with versioning and retention.
5. Create GitHub environment `staging` with the deploy credentials and S3 keys. The S3 key used for the conformance run (`mise run infra:s3-conformance`, PR #260) needs rights to create and delete buckets, set versioning, use object lock and bypass governance retention.
   From the prod overlay (PR #263), also needed for staging and later production:
   - the backup key must not be able to delete; create one separate admin key (`BACKUP_ADMIN_S3_*`) and run `mise run infra:backup-lifecycle` once against the real backup bucket, then `mise run infra:backup-preflight` to verify (base backups expire after 9 days, WAL after 10; tested only against RustFS so far);
   - the deploy job needs one successful `pr-ready` run on `main` first (it deploys the image digests that run published), which in turn needs X1;
   - set `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_SSH_KEY` and `DEPLOY_KNOWN_HOSTS` in the GitHub environment; the deploy job skips itself until they exist;
   - set up the VM by `docs/infra/hardening.md`, run `SELFHOST_ENV_TEMPLATE=infra/deploy/.env.prod.example bash infra/deploy/selfhost.sh init`, add the Hetzner secrets to the env file, and move the backup recovery key off the VM;
   - **Decision D7: container images.** Make the GHCR packages public (simplest; images hold no secrets), or keep them private and give the VM a read-only token. Suggested: private with a read-only token.
   - **Decision D8: where alerts go.** Prometheus evaluates the alert rules but nothing sends them. Options: email, Telegram, or a hosted service such as Grafana Cloud's free tier. Say which and I will wire it.
6. Cloudflare (#235): activate the zone, proxy the API record, serve the web client from Workers static assets, and keep the deploy token in GitHub environment secrets.
7. Only if D1 = b: record the Railway Postgres facts listed in #232.

Blocks: the real-bucket run of #236, the staging parts of #239, and #241.

### C2. Backup and recovery keys ([#27](https://github.com/datamonsterr/duolify/issues/27))

1. **Decision D5: who holds the backup recovery key** (`BACKUP_AGE_IDENTITY`)? Default: only you, stored offline, with the public recipient in the environment.
2. After I1, provision the separate identities as environment secrets: writer (`BACKUP_S3_*`), expiry (`BACKUP_EXPIRE_S3_*`), restore (`BACKUP_RESTORE_S3_*`), asset recovery (`ASSET_RECOVERY_S3_*`), plus `BACKUP_AGE_RECIPIENT` and the `BACKUP_FINANCIAL_FIXTURE_*` values.

### C3. Live sign-in ([#48](https://github.com/datamonsterr/duolify/issues/48))

1. Resend: account, verified sending domain (depends on D4), `RESEND_API_KEY`, `AUTH_MAIL_FROM`.
2. Apple Sign in: `APPLE_CLIENT_ID`, `APPLE_TEAM_ID`, `APPLE_KEY_ID`, `APPLE_PRIVATE_KEY`. Needs an Apple Developer account.
3. Google OAuth application. The code has no `GOOGLE_*` variables yet; I will confirm the names before you create it.
4. A production `JWT_SECRET`.
5. Manual check: sign in with email, Google and Apple on a desktop build and a mobile build.

### C4. Live narration voices ([#116](https://github.com/datamonsterr/duolify/issues/116))

Blocked by C1.

1. **Decision D6: launch languages and ElevenLabs voice.** The default model is `eleven_flash_v2_5`; no voice is chosen.
2. Provide `ELEVENLABS_API_KEY` and `ELEVENLABS_VOICE_ID`.
3. Confirm that your ElevenLabs plan allows redistributing narration in shared courses (asked in ADR 0013 and #37, no answer recorded).
4. Manual check: listen to the live narration samples and approve clarity and caption timing.
5. Confirm a second voice vendor stays deferred. Default: yes, captions-only fallback.

### C5. Stripe account and keys ([#24](https://github.com/datamonsterr/duolify/issues/24))

1. Create the Stripe account (you as individual merchant of record, Stripe Tax on, per ADR 0013).
2. Provide `STRIPE_PRICING_TEST_SECRET_KEY`, `STRIPE_PRICING_LIVE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET` and the product and price IDs through the environment secret store.
3. Confirm two points from the issue that have no recorded answer: small-business eligibility, and that paid credits never expire (a standing liability).

Blocks #111 and #145.

### C6. Analytics keys ([#54](https://github.com/datamonsterr/duolify/issues/54))

PostHog project and keys: `POSTHOG_API_KEY`, `POSTHOG_PROJECT_ID`, `POSTHOG_HOST`, `VITE_POSTHOG_KEY`. Also say who may approve a production deploy. Default: only you.

## P2 — launch and pilot

### A2. Default character art ([#40](https://github.com/datamonsterr/duolify/issues/40))

Approve the names and art of the three built-in characters, still called "Provisional Glint", "Provisional Moss" and "Provisional Echo", across the five expressions (idle, happy, sad, thinking, celebrate). The review sheet is in PR #262 (`docs/design/characters/default-cast/sheet.png`). Answer three questions: approve the art or give changes; the final names; confirm no third-party likeness. Notes from review: Moss's idle mouth reads as surprised, and Echo's eyes look nearly the same in four of the five expressions.

### A3. Launch catalogue ([#111](https://github.com/datamonsterr/duolify/issues/111))

Blocked by C5.

1. Approve or revise the budgets: 6,000 USD per month runway and growth, and 50 USD per day platform spend.
2. Approve or revise the plan assumptions (six lessons per month, shared free demo, paid custom previews).
3. Verify the real Stripe product IDs and localized prices.
4. Verify competitor comparison claims or drop them. Default: drop them.
5. At deploy: set `PRICING_OWNER_ID` and `PRICING_ADMIN_IDS` to your account, then approve and schedule pricing versions `usd-design-chat-owner-approved-2026-11-16` and `usd-character-create-2026-11-16`. Character creation and design chat fail closed until then.

### A4. Sharing release ([#37](https://github.com/datamonsterr/duolify/issues/37))

1. Legal approval for sharing (`SHARING_RELEASE_LEGAL_APPROVED`).
2. CDN policy approval (`SHARING_RELEASE_CDN_POLICY_APPROVED`), after D4.
3. Your account as moderator. I can add a small operator command for the `moderator_grants` row; say if you want it.

### M1. Stripe sandbox check and paid pilot ([#145](https://github.com/datamonsterr/duolify/issues/145))

Blocked by C1, C5 and A3. On the Stripe sandbox, check pack, monthly and annual purchase, cancel, renewal, refund and restore with no duplicate grants. Then approve the pilot limits and the territories.

### M2. Production deploy ([#242](https://github.com/datamonsterr/duolify/issues/242))

Provision production VMs in `hel1`, create GitHub environment `production` with its secrets, and run the runbook from #241. With D1 = a this is a first deploy, not a migration.

## Housekeeping — a yes or no each

- **H1.** Close or relabel the parked tickets: Apple billing (#143), Google billing (#144) and the marketplace pilot (#147). ADR 0013 parks them, but they still say `ready-for-human`. Suggested: label `parked` and remove `ready-for-human`.
- **H2.** Remove the 36 local branches already merged into `main` and their worktrees. None are deleted without your yes.
- **H3.** ADR 0011 has a table of defaults chosen by agents during implementation. ADR 0013 accepted the ADR 0012 defaults but not this table. Review it or say "accepted".
- **H4.** `docs/status.md` is stale (2026-11-14). I will refresh it unless you prefer to delete it.
- **H5.** Agent capacity: all Claude agents share one usage limit with the master session. `codex-2` has free quota. Allow coding tickets on `codex-2`, or keep Claude only (slower, about three agents at a time)?

## Not needed now

- Apple and Google developer accounts for in-app billing (#143, #144): parked by ADR 0013.
- Push credentials (APNs, FCM): not needed for v1.
- Marketplace pilot (#147): revisit after link-sharing feedback.
