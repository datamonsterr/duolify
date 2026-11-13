## Token

20 bytes from `crypto/rand` encoded as base64url (160 bits, exceeds the >=128-bit requirement). Stored **only** as `SHA-256(token)` hex in `publication_links.token_hash`. The plaintext token is returned once — on creation — and never persisted. Rotation generates a new token and immediately marks the previous row `revoked_at = now()`.

## Visibility

`courses.course_visibility` is an enum `private | link` (default `private`). The value `public` (marketplace) is reserved for E15-03+. A Course may be published only when its latest `publication_drafts` row has `verdict = pass`.

## publication_links table

```sql
CREATE TABLE publication_links (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  course_id   UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
  token_hash  TEXT NOT NULL UNIQUE,          -- SHA-256 hex of the plaintext token
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at  TIMESTAMPTZ                    -- NULL = active; set = revoked
);
CREATE INDEX ON publication_links(course_id) WHERE revoked_at IS NULL;
```

At most one active link per course (enforced by partial unique index on `(course_id)` where `revoked_at IS NULL`).

## Public Preview Payload

Allowed fields returned by `GET /preview/{token}`:

| Field | Source |
|---|---|
| `course_id` | courses.id |
| `title` | courses.title |
| `description` | courses.description |
| `version` | publication_drafts.number |
| `lesson_count` | count of lessons in draft snapshot |
| `generated_lesson_count` | count of lessons with generated slides |
| `creator.username` | learners.username |
| `creator.display_name` | learners.display_name |
| `creator.avatar_url` | learners.avatar_url (omitempty) |
| `outline` | units + lesson titles from draft snapshot |
| `sample_slides` | up to 3 slides from first generated Lesson (HTML only) |
| `enrollment_count` | aggregate integer from course_enrollments |

**Excluded from preview (never returned):** questions, `answer_index`, `explanation`, `retry_pool`, narration refs, narration audio, steering instructions, private notes, chat history, Learner progress.

## noindex

- HTTP header: `X-Robots-Tag: noindex`
- HTML preview page: `<meta name="robots" content="noindex">`
- Cache-Control: `no-store` on the preview endpoint

## Rate Limiting

Per-IP sliding window on `GET /preview/{token}`. Config env vars:

| Var | Default | Meaning |
|---|---|---|
| `SHARING_PREVIEW_RATE_LIMIT_PER_IP` | `60` | Max requests per window |
| `SHARING_PREVIEW_RATE_WINDOW_SECONDS` | `60` | Window size in seconds |

IP resolved from `X-Forwarded-For` (first entry) or `RemoteAddr`. Exceeding the limit returns HTTP 429.

## Block Check

If the signed-in viewer and the Course owner have blocked each other (`BlockedEitherWay`), `GetPreview` returns `ErrLinkNotFound` — a neutral 404 — revealing nothing about the Course or its owner (social contract E6-04).

## Deep Link

`SHARING_APP_SCHEME` env var (default `"duolify"`). Preview page renders:

```html
<a href="${scheme}://share/${token}">Open in Duolify app</a>
```

## Domain & Preview URL

`SHARING_PUBLIC_DOMAIN` env var (default `"localhost"`). Preview URL:

```
https://${domain}/preview/${token}
```

Returned to the owner on publish and rotate as `preview_url`.

## Config Summary

```
SHARING_PUBLIC_DOMAIN               (default: localhost)
SHARING_APP_SCHEME                  (default: duolify)
SHARING_PREVIEW_RATE_LIMIT_PER_IP   (default: 60)
SHARING_PREVIEW_RATE_WINDOW_SECONDS (default: 60)
```

All parsed in `ConfigFromEnv()` in `services/api/internal/sharing/config.go`.
