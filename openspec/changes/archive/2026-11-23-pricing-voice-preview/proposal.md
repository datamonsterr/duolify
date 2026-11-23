# Pricing, voice and preview

## Why
The owner approved a larger Course preview envelope and a low-cost transcription route on 2026-11-23. Check-in Credits must fund generation independently of the Daily free allowance cap.

## What Changes
- Raise preview output to 6,000 tokens with the learner price unchanged and align evaluation with the served envelope.
- Configure private OpenRouter transcription with a bounded free request and a fallback.
- Apply the free-learner daily USD cap only to the Daily free allocation, including mixed reservations.
- Report truncated preview and Lesson output as an explicit generation failure.

## Impact
Pricing, Credits accounting, Engine routing and evaluation. The voice endpoint and client are future work. Existing backup and recovery coverage applies; no audio or transcript is stored for this routing configuration. Analytics for a future endpoint record requested, succeeded, failed and fallback-used events with only duration bucket and model. Transcription reliability uses the recorded spike, private routing and fallback; generation uses the golden set and bounded output.
