# Interaction flows

These are design contracts for subsequent implementation. The prototype exercises the lesson/test and illustrative purchase boundaries with authored fixtures; production grading, costs and rollover are server-owned. Every AI path preserves work, enforces configured caps and uses the existing timeout/retry/fallback policy. No flow grants credits for effort.

## Course creation: preview, outline, then gated lessons

```mermaid
flowchart TD
  A[Courses: Create a course] --> B[Topic, optional profile and level]
  B --> C[Show preview estimate and available credits]
  C --> D{Enough credits and explicit consent?}
  D -->|No balance| SHOP[Shop or wait for daily reset; retain topic]
  SHOP --> C
  D -->|Yes| E[Reserve preview estimate; request sample slides and outline]
  E --> F{Validated preview ready?}
  F -->|No| FAIL[Timeout, retry once, fallback; release on terminal failure]
  FAIL --> B
  F -->|Yes| G[Settle preview usage; release unused reservation]
  G --> H[Preview examples; no course or enrollment persisted]
  H -->|Edit| B
  H -->|Discard| X[Return to Courses; no course created]
  H --> I[Review sample units, lesson titles and goals]
  I --> J[Confirm outline cost, spend order and limits]
  J --> K{Enough credits and confirm?}
  K -->|No balance| SHOP2[Shop or wait; retain preview context]
  SHOP2 --> J
  K -->|Yes| L[Reserve estimate; enqueue idempotent outline-only job]
  L --> M{Valid outline?}
  M -->|No| N[Show retry; release reservation; no partial enrollment]
  N --> J
  M -->|Yes| O[Store outline and enrollment; settle actual usage]
  O --> P[Learn path: first lesson ready to generate]
  P --> Q[Estimate includes narration; request eligible lesson only]
  Q --> R[Generate lesson 1; no future lesson content yet]
  R --> S[After test pass, enqueue next lesson only]
```

Back/Cancel preserves the local draft but does not claim server persistence of a discarded preview. A price estimate that changes requires renewed confirmation. The API refuses gated or unaffordable generation before calling the engine. Duplicate submits/reconnects attach to the same job and reservation.

## Lesson, test and retry variants

```mermaid
flowchart TD
  A[Open eligible cached or generated lesson] --> B[Paused Motion HTML; captions and transcript available]
  B --> C[Play, pause, seek, speed and mute share one timeline]
  C --> D{Learner action}
  D -->|Tutor or quick note| SHEET[Pause; preserve slide position; open context sheet]
  SHEET --> C
  D -->|Previous or next slide| C
  D -->|Exit| PATH[Save position; return to path]
  D -->|Last slide: Take lesson test| E[Load original questions; no answer key in client]
  E --> F[Select answer; explicit Check answer]
  F --> G{Online server grading available?}
  G -->|No| WAIT[Keep attempt pending; no XP or next-generation unlock]
  WAIT --> G
  G -->|Yes| H{Correct?}
  H -->|No| I[Show correction; queue fresh paraphrased prompt and choices]
  H -->|Yes| J[Mark topic resolved; remove resolved variant obligation]
  I --> K[Explicit Continue; other original topics before retry variants]
  J --> L{Original topics unresolved or retry queue nonempty?}
  L -->|Yes| K
  K --> M{Next question ready?}
  M -->|Yes| F
  M -->|No: variant failure| RETRY[Keep queue; Retry or Return to lesson; never auto-pass]
  RETRY --> M
  L -->|No| N[Verify pass and record completion once]
  N --> O[Award XP and local-day streak; effort-earned freeze if milestone]
  O --> P[Finish screen; next lesson generation request]
  P --> Q{Enough credits?}
  Q -->|Yes| R[Enqueue only N+1; show generation status]
  Q -->|No| S[Keep completion; offer credits or wait]
```

Resolved-topic progress is not a decreasing percentage or a lives counter. A wrong variant queues another fresh variant. When no other topic remains, the persistent correction and Continue separate the retry from the mistake. Replaying a completed lesson cannot duplicate XP or generation. Server completion, not merely reaching the end of slides, is authoritative.

## Credits exhaustion, purchase and return

```mermaid
flowchart TD
  A[AI action with estimate] --> B{Enough available daily plus paid credits?}
  B -->|Yes| C[Reserve; daily free spent first]
  C --> D{AI outcome}
  D -->|Complete| E[Settle actual usage; release remainder]
  D -->|Failed| F[Release full reservation]
  D -->|Chat stopped or interrupted after tokens| PART[Settle produced usage; release remainder]
  B -->|No| G[Refuse AI call; keep learner work]
  G --> N[Raw quick note saves with enrichment pending]
  G --> H{Choose recovery}
  H -->|Review saved content| X[Cached lessons, notes and bookmarks remain usable]
  H -->|Wait for reset| I[Show local reset time; refresh authoritative grant]
  H -->|View options| J[Credit packs or monthly/yearly subscription]
  J --> K[Show full billed amount, period, taxes and terms]
  K --> L[Explicit checkout; platform-compliant payment flow]
  L --> M{Return state}
  M -->|Cancelled or failed| O[No balance change; preserve draft; Retry or return]
  M -->|Pending| P[Payment pending; check status; no credit grant yet]
  P --> M
  M -->|Verified webhook and ledger update| Q[Refresh wallet; show added credits once]
  Q --> R[Offer Resume action; require explicit send or generation consent]
  R --> A
  I --> A
```

Only a verified idempotent webhook posts purchased credits. Query flags, browser closure and elapsed time never prove payment. Daily credits expire at the learner's local day-end and are one shared pool sized for approximately 3 note enrichments plus 5–8 chat messages, not fixed separate counters. E12 supplies actual offers and platform billing rules. The prototype cannot charge money.

## Automatic streak freeze application

```mermaid
flowchart TD
  A[Idempotent server rollover for learner-local day] --> B[Reconcile completed lesson events]
  B --> C{At least one verified completion that day?}
  C -->|Yes| D[Count completed day once; update streak]
  D --> E{Effort milestone and below freeze cap?}
  E -->|Yes| F[Grant one freeze once; no credits]
  E -->|No| G[Keep inventory; no credit substitute]
  C -->|No| H{Owned freeze available?}
  H -->|Yes| I[Consume one freeze atomically; preserve streak length]
  I --> J[Persistent notice: freeze covered local date; remaining count]
  H -->|No| K[Reset current streak; preserve longest streak]
  K --> L[Welcome-back message; offer one lesson]
  F --> M[Refresh calendar and inventory on next client sync]
  G --> M
  J --> M
  L --> M
  M --> N[Friend streak rollover waits for later partner day-end]
  N --> O[Apply configured friend protection policy without duplicate consumption]
```

A freeze preserves a missed day; it does not add XP or increase the streak. No manual activation or credit purchase is designed. Friend-streak protection/consumption is an open E6 policy question: final copy and inventory behavior require that decision. Offline views show last verified state, not optimistic freeze use. Timezone changes must reconcile through server policy; client midnight timers never finalize the result.
