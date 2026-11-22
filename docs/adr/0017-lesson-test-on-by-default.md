# ADR 0017: The lesson test is on by default

- Status: Accepted (controller, 2026-11-22)
- Date: 2026-11-22

## Context

The lesson test, direct lesson generation and generated-lesson publication sat behind `LESSON_TEST_TRACER_ENABLED` (API, default `false`) and `VITE_LESSON_TEST_TRACER_ENABLED` (client, default off). `.env.example` set both to `false` and no deploy file set them, so every real deployment hid the lesson test. That contradicts `openspec/specs/lesson-test`, where the test ends every Lesson.

## Decision

The lesson test is a core feature and is on in every environment by default. The flag names stay; the flag is only an explicit kill switch. Only an explicit `false` disables it. An unset value is enabled; an unparseable API value still fails startup.

## Consequences

- API `config.Load` defaults `LessonTestTracerEnabled` to true; the client resolver returns true when neither env nor server says otherwise. `/v1/features` reports the server value and the client prefers it.
- When enabled, the API serves `/v1/lessons/*` and `/v1/lesson-generations/*`, the worker runs the lesson queue and the one-off generated-publication backfill, and Course sharing can generate missing pinned Lessons.
- Compose files (`docker-compose.yml`, `infra/deploy/compose.yml`) pass `LESSON_TEST_TRACER_ENABLED` through with default `true`.
- Deployments need a reachable Engine before upgrading, since the worker now starts the lesson queue.

## How to disable

Set `LESSON_TEST_TRACER_ENABLED=false` on the API and worker, and build the client with `VITE_LESSON_TEST_TRACER_ENABLED=false`. The routes then return 404, `/v1/features` reports `false` and the client hides the test.
