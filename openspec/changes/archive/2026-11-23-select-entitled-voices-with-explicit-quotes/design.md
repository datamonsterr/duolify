## Decisions
- Approved set in config, not code: both configs list logical keys; the API test compares them. The quote policy of a Pricing version stays immutable; `pricing.AllowVoices` merges the approved voices into the policy used for the quote, and the snapshot records the merged list.
- Consent is bound to the quote: the apply call recomputes the characters still to synthesize, requires an unexpired quote that covers them and the same voice-config hash, accepts it, and the River job carries the quote id. The job reserves against that quote; an expired or too small quote disables the new audio with `quote_expired`, never silently re-quotes.
- Setting change never charges: the preference is stored at consent time and charges only through the accepted quote. A curated preference that is no longer entitled falls back to the default voice for new narration.
- Retry (insufficient credits) is not offered for a voice change; the Learner reviews a fresh quote again (`voice_insufficient_credits`).
- Recovery from a missing object only repoints to a retained version of the same vendor voice, so a voice is never swapped silently.
