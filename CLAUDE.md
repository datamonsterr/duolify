@AGENTS.md

## CI/CD Resource & Push Policy
This account shares 40 concurrent GitHub Actions slots across all repos and a 3,000 min/month private-repo budget. Treat CI as a final verification gate, NOT a scratchpad.

1. **Verify locally before pushing:** Run the repo's local check task (`mise run check`, `cargo clippy`, `pnpm lint && pnpm typecheck && pnpm test`) and confirm it passes before `git push`.
2. **Batch commits:** Do not push after every small edit. Commit locally and push only when a logical milestone passes local checks.
3. **Draft PRs first:** Always open new PRs as `--draft` (`gh pr create --draft`). Only mark ready (`gh pr ready`) once local checks pass and implementation is complete.
4. **No unnecessary heavy triggers:** Do not add `e2e` or `bench` labels unless explicitly requested.
5. **Workflow hygiene:** Every new GitHub Actions workflow MUST include `concurrency` (`cancel-in-progress: true`), `timeout-minutes` on every job, and path filters. Never add `macos-*` or `windows-*` jobs to private PR triggers.
