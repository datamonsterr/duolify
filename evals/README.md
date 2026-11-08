# Golden set

Each JSONL row has an `id`, `task` (preview, outline, lesson, chat or note), JSON `input`, textual `rubric`, and `checks`. Check kinds are `json`, `field`, `slide`, `contains`, and `regex`; `field`, `contains`, and `regex` use a `value` string. A case passes when all checks pass. `thresholds.toml` sets the required fraction of passing cases.

Create a dataset on Hugging Face and upload `golden.jsonl` at its root. Set `HF_GOLDEN_DATASET`, `HF_GOLDEN_REVISION` to a commit hash, and `HF_TOKEN` with dataset read access. Run `mise run engine:eval-live` with provider keys. The runner downloads that revision into `evals/.cache`; reports are written to `evals/reports`. Use `mise run engine:eval` for the offline mock fixtures. `--judge` additionally requests a PASS/FAIL judgment against the rubric.
