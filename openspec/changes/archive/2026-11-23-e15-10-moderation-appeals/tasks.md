## 1. Tests first
- [x] 1.1 E2E: moderate a borderline publication, take down severe content, appeal once
- [x] 1.2 Integration: roles, audit, copy access, appeal separation
- [x] 1.3 API and client unit boundaries
- [x] 1.4 Integration: overturned takedown re-activates the share link (issue #207); owner-revoked link not re-activated; audit row present; rollback leaves nothing half-done
- [x] 1.5 E2E: original share URL works again after overturn (issue #207)
## 2. Implementation
- [x] 2.1 Analytics catalogue before emitters
- [x] 2.2 Migration, service and routes
- [x] 2.3 Copy blocking in player and course reads
- [x] 2.4 Moderator queue, action dialog, author notice and appeal UI
- [x] 2.5 Backup, privacy, inventory, anonymizer and restore checks
- [x] 2.6 Focused mise verification and conventional commits
- [x] 2.7 Fix #207: add `revoked_by_action_id` to `publication_links`, re-activate on overturn, write `restore_link` audit row in same transaction
