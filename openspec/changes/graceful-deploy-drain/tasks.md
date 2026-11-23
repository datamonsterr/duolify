## 1. Specification and tests
- [x] 1.1 Validate the change; write failing tests first (Go drain, River interruption, Engine drain, deploy order, client retry).

## 2. Implementation
- [x] 2.1 API: bounded drain, `/readyz` 503 while draining, shared queue drain then hard stop.
- [x] 2.2 Engine: NOT_SERVING on SIGTERM, bounded drain (`serve_with_drain`).
- [x] 2.3 API to Engine gRPC retry on `UNAVAILABLE`.
- [x] 2.4 deploy.sh: overlapped Engine roll, `docker stop --timeout 120`; compose grace periods; deploy-plan rule.
- [x] 2.5 Client: one fresh-quote retry for transient preview failures.
- [x] 2.6 Sync delta specs to `openspec/specs/`; `mise run ci:openspec`.
