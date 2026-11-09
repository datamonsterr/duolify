# Duolify brand

Duolify makes room for a small discovery every day. It is a playful, patient learning companion: bright, curious, precise, optimistic and respectful of a learner's time. The look is Duolingo-style — chunky, colorful and game-like — but the brand, name, logo and illustrations are entirely Duolify's own. The product can teach astronomy, gardening or distributed systems with the same calm structure. It celebrates understanding and returning, rather than speed or perfection on the first try.

## Voice and tone

Use short, concrete sentences. Address the learner as “you.” Explain what happens next before asking for commitment. Praise an action, never make claims about intelligence. Corrections teach; streak messages welcome a return without guilt. Say when generated material is uncertain. Keep celebration brief, sound optional and prices explicit.

| Moment | Example |
| --- | --- |
| Welcome | “What would you like to keep learning?” |
| Resume | “A little closer to the cosmos.” |
| Correct | “That's it. Earth's rotation changes your view.” |
| Incorrect | “Earth turns once each day. You'll try this idea again in a fresh question.” |
| Complete | “One idea clearer. Your next lesson is getting ready.” |
| Missed day | “Welcome back. Start with one small lesson.” |
| Freeze | “A streak freeze covered yesterday. Your 7-day streak is safe.” |
| Credits | “Your note is saved. Add credits to enrich it, or wait for your daily reset.” |
| Failure | “This lesson couldn't be prepared. Your reserved credits were released. Try again.” |
| Uncertain tutor | “I don't have enough context to be sure. Let's revisit the lesson's example.” |

Avoid “You failed,” guilt, countdown pressure to buy, unqualified “free,” childish pet names and lives/hearts metaphors. Do not call a payment pending state “success.” Never claim that effort earns credits.

## Product language

`CONTEXT.md` is the glossary. Use sentence case in UI, preserving XP and Motion HTML. “Duolify” is the product name in titles; the text logo may be lowercase `duolify`. Never substitute names for the core domain concepts.

| UI concept | Label and explanation |
| --- | --- |
| Learner / learner profile | “Profile”; “Your background helps tailor examples. You can skip this.” |
| Course / course outline / unit | “Courses,” “Course outline,” “Unit 2.” A course is a lasting track, a unit groups lessons. |
| Course preview | “Preview your course”; sample slides and sample outline before creation. |
| Lesson / slide / lesson test | “Lesson 3,” “Slide 2 of 5,” “Lesson test.” Slides are Motion HTML, never video. |
| Retry variant | “Retry variant”; “Same idea, fresh wording.” Explain this on first mistake. |
| Gated generation | Internal term; learner copy: “Pass this lesson test to prepare the next lesson.” |
| XP / daily goal / league | “XP,” “Daily goal,” “Weekly league.” XP records effort; it is not money. |
| Streak / streak freeze / friend streak | Keep the full names at first mention. Freezes are earned through effort and applied automatically. |
| Credits / daily free credits / subscription | “Credits,” “Daily free credits,” “Subscription.” Show expiry, spend order and billing period. |
| Narration / captions | “Narration,” “Captions,” “Transcript.” Never imply captions require unmuted audio. |
| Tutor chat / quick note / bookmark | “Ask tutor,” “Add quick note,” “Bookmark slide.” Saved material remains accessible at zero credits. |

## Logo direction

A compact lowercase wordmark with open counters and a slightly lifted dot over the “i.” Accompany it with two offset rounded tiles forming an opening, suggesting one idea making room for another. The standalone mark is two solid geometric tiles, never an animal, eye pair, shield or owl. Start with text and CSS geometry in the prototype; commission the final vector after legibility review at 16px and app-icon sizes. Use text as the accessible name and hide the decorative mark. Maintain one cap-height of clear space. No final logo asset is claimed here.

## Palette and illustration

Green is the action color (theme revision, 2026-11-09): a deep, readable green for buttons and links, with a bright lime used only for progress fills and illustration. Sky blue marks selection, secondary actions and information; sun yellow marks completed lessons, XP and the daily goal; flame orange is the streak; gem violet is credits. Labelled forest, red and amber carry success, correction and attention. Dark mode uses deep teal-black surfaces with bright green actions. Exact values and approved pairings live in the design system and [tokens.json](tokens.json).

Use original, chunky geometric illustration: rounded tiles with a solid darker edge, orbital lines, simple shapes and small annotations. Illustrations explain a concept, with a text equivalent; they never carry essential information alone. A course can vary its shapes, but action colors stay constant. Avoid mascots, characters, borrowed assets, owl or bird silhouettes, Duolingo's name, logo, fonts or copy. The [reference repository](https://github.com/sanidhyy/duolingo-clone) informs only sidebar, learning-path rhythm and tactile button patterns; no assets, code or text are reused.

## Conversational setup

Onboarding and course creation are guided chats, not forms. The assistant asks one short question at a time, offers tappable quick replies, asks follow-ups only when an answer is vague, and lets the learner skip anything. Before any paid step it shows a plain summary of what it understood and waits for the learner to confirm or edit it. Assistant copy follows the voice above: friendly, brief, never pushy.

## Iconography

Use a consistent 24px grid, 2px rounded strokes and open shapes. Use a compass/tiles for Learn, stack for Courses, paired circles for Friends, flag for Leagues, page for Notes, bookmark for Bookmarks and outlined diamond for Credits. Directional icons mirror in RTL; play and media time do not. Pair icons with visible labels in navigation. Icon-only controls require accessible names and tooltips on hover and focus. Emoji are not the production icon set.

## Sound and haptics

Narration is the primary audio. It begins only after Play. Optional UI cues use a soft wood tap for selection and two rising marimba notes for completion; corrections use a quiet neutral tap, never a buzzer. UI sounds duck below narration and can be disabled independently. No automatic audio on entry, no repeating purchase sound. All audio information has a visible equivalent.

Use one light haptic for a confirmed press and a short paired pulse for completion when supported. Never haptically punish mistakes. Respect OS haptics and sound settings. Reduced motion suppresses celebration movement; haptics and sound remain separate opt-in controls. Desktop web prototype has no sound or haptics and explicitly labels the fake narration timeline.

## Open design questions

1. E12 must decide pack sizes, real prices, subscription benefits, localized taxes/store flows and exact daily grant amounts; all prototype offers are placeholders.
2. E6 must resolve shared-day labels and whether a personal freeze also protects a friend streak without double consumption. Show no toggle that promises an unconfirmed policy.
3. E14 must validate caption granularity and voice intelligibility across locales; offer narration and caption preferences without exposing provider model IDs.
4. Final wordmark artwork, sound samples and learner testing (including screen reader users) remain future implementation work.
