# Control and resume slide playback

## Why
E3-02 (#73) needs working controls and a learner-owned resume cursor without awarding lesson completion.

## What changes
Add a parent playback clock and sandbox animation bridge, source/schema checked ended/checkpoint handling, explicit controls, pause on hidden pages and sheets, safe originating-path exit, and an authorized Postgres cursor API with disposable learner-scoped device recovery. Register content-free analytics before emitters. Cache/preload and durable offline event reconciliation remain E3-03/E3-04.

## Impact
Client player, API player, learner privacy/export, encrypted nightly backups and fixture restore checks. No AI/provider or proto changes; existing eval/fallback gates remain unchanged.
