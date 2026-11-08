# Duolify glossary

One term, one meaning. Use these words in code, specs and UI copy.

| Term | Meaning |
|---|---|
| **Learner** | A signed-in person who takes courses. |
| **Course** | A long-term learning track on one topic (like one language in Duolingo). A learner can enroll in many courses. |
| **Course outline** | The generated structure of a course: units and lesson titles with goals. Generated first, before any lesson content. |
| **Unit** | An ordered group of lessons inside a course. |
| **Lesson** | One short learning session. Content is a deck of slides plus a lesson test. Generated only when the learner reaches it (see *Gated generation*). |
| **Slide** | One small, self-contained Motion HTML document that plays an animation of a few seconds and can pause, replay and step. A lesson has many slides. |
| **Motion HTML** | HTML + CSS + JS animation content, rendered live in a sandboxed webview. Never rendered to MP4. |
| **Slide manifest** | JSON that lists a lesson's slides, durations, assets and checkpoints. |
| **Lesson test** | Quiz at the end of a lesson. The learner passes only by answering every question correctly. A wrong answer shows the correction, then a *retry variant* is queued. |
| **Retry variant** | A paraphrased question on the same topic as a missed question, with paraphrased answer choices. Asked later in the same lesson test; it must be answered correctly to pass. |
| **Gated generation** | The engine generates lesson N+1 only after the learner passes lesson N's test. |
| **Course preview** | Example slides and a sample outline shown before the learner commits to generating a course. |
| **XP** | Points earned by completing lessons. Drives leagues and daily goals. |
| **Streak** | Count of consecutive days with at least one completed lesson. |
| **Streak freeze** | Item that preserves a streak for one missed day. Earned by effort (streak milestones), applied automatically on a missed day. |
| **Friend streak** | Shared streak between two learners; it grows when both complete a lesson on the same day. |
| **Credits** | Balance that pays for AI features (generation, chat, notes, narration). Stored in an append-only ledger. Effort is never rewarded with credits. |
| **Daily free credits** | Credits granted every day to every learner, sized to cover 3 quick notes and 5 to 8 tutor chat messages. They expire at the end of the day and are spent before purchased credits. |
| **Subscription** | Paid monthly or yearly plan that grants a monthly credit allowance and extra benefits (see the monetization epic). |
| **Narration** | Spoken audio for a slide, generated from the slide's narration text with the ElevenLabs API and played in sync with the slide. |
| **Tutor chat** | Chatbot with context of the whole course, the current lesson, the learner profile and history. |
| **Quick note** | Short learner note, auto-enriched by AI (summary, links, tags), easy to review. |
| **Bookmark** | Saved pointer to a slide or lesson. |
| **Learner profile** | Background, goals and level the learner shares; used as AI context. |
| **Golden set** | Versioned evaluation cases for AI features, stored as a Hugging Face dataset. |
| **API** | `services/api`, the Go service that clients call. |
| **Engine** | `engine/`, the Rust service that runs AI agents. Only the API calls it. |
