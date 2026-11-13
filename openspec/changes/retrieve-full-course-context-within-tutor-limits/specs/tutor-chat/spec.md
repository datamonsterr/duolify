## ADDED Requirements

### Requirement: Full-course tutor context within limits
The tutor context MUST be assembled statelessly per turn from the authenticated Learner's own data for the Course: outline, every lesson summary, the effective profile, recent messages and a bounded set of source excerpts retrieved from older generated lessons that match the question. Retrieval and summaries MUST NOT include another Learner's or another Course's content, deleted threads or deleted lessons. Retrieval queries MUST be parameterized. Summaries MUST NOT be dropped silently; the prompt MUST stay within the quote's input bound, which stays under the configured prompt-token limit. When the mandatory parts exceed the standard bound but fit the limit, the API MUST answer a typed `context_approval_required` and only an explicit Learner approval bound to a new quote MAY proceed; above the limit it MUST answer `context_too_large`. Neither case charges Credits.

#### Scenario: Ask about the oldest lesson
- **WHEN** a Learner asks about a topic taught only in the first lesson of a large Course
- **THEN** the prompt contains that lesson's excerpt and every summary and the reply is grounded in it

#### Scenario: Oversized Course
- **WHEN** all summaries together exceed the standard bound
- **THEN** the oldest summaries are kept, the turn does not run, nothing is charged and the response is the typed approval or too-large answer

#### Scenario: Approved large context
- **WHEN** the Learner approves and the quote is created with `approve_large_context`
- **THEN** the send runs under the larger Pricing-derived bound at the same credit price

#### Scenario: Injection in an old lesson
- **WHEN** a retrieved lesson says to ignore instructions
- **THEN** it stays inside the untrusted data block and the tutor keeps grounding, refusal and safety behaviour

#### Scenario: Foreign or deleted content
- **WHEN** another Learner's lesson, another Course's lesson or a deleted lesson matches the question
- **THEN** it is never retrieved or summarised
