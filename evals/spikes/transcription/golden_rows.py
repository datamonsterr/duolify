#!/usr/bin/env python3
"""Turns manifest.json plus the audio built by prepare.py into `transcribe` golden rows (task 6.7).

  python golden_rows.py --audio audio --stage stage

Writes stage/audio/<id>.<container> (the files to upload to the Hugging Face dataset) and prints one JSONL row per
clip and container to stdout, to append to evals/datasets/duolify-golden/golden.jsonl. Each row carries the file's
sha256, so the digest-pinned golden.jsonl pins the audio bytes too. Scoring rules live in engine-core
`transcribe::{normalized, wer}` (same method as run.py). Nothing here prints a secret.
"""
import argparse, hashlib, json, os, shutil

# Per-clip ceilings (mean targets: English 6 %, Vietnamese 12 %; see evals/README.md, "Transcription cases").
# Clips also measured on the fallback model alone (the primary is forced to fail), silence included.
FALLBACK = [("en-clean-1", "webm"), ("en-accent-1", "m4a"), ("tech-1", "webm"), ("vi-1", "m4a"), ("es-1", "webm"),
            ("silence-1", "webm"), ("silence-1", "m4a"), ("noise-only-1", "webm"), ("noise-only-1", "m4a")]
WER = {"clean_en": .20, "accented_en": .20, "noisy_en": .20, "technical_en": .20, "long_en": .20, "short_en": .50,
       "spanish": .20, "vietnamese": .40}
MAX_COST_USD = 0.0005


def pe(path, expected):
    return {"kind": "path_equals", "critical": True, "value": json.dumps({"path": path, "expected": expected})}


def main():
    ap = argparse.ArgumentParser(); ap.add_argument("--audio", required=True); ap.add_argument("--stage", required=True)
    a = ap.parse_args()
    manifest = json.load(open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "manifest.json")))
    os.makedirs(os.path.join(a.stage, "audio"), exist_ok=True)
    for c in manifest["clips"]:
        category = {"vi": "vietnamese"}.get(c["category"], c["category"])
        silent = not c["ref"]
        for container, forced in [(x, False) for x in ("webm", "m4a")] + [(x, True) for (i, x) in FALLBACK if i == c["id"]]:
            src = os.path.join(a.audio, f"{c['id']}.{container}")
            data = open(src, "rb").read()
            dst = os.path.join(a.stage, "audio", f"{c['id']}.{container}")
            shutil.copyfile(src, dst)
            checks = [pe("delivered", True), pe("silence_ok" if silent else "wer_within", True),
                      pe("latency_within", True), pe("cost_within", True)]
            row = {"id": f"live-transcribe-{c['id']}-{container}" + ("-fallback" if forced else ""), "task": "transcribe",
                   "input": {"category": category, "language": c["lang"] or "", "reference": c["ref"],
                             "audio": {"file": f"audio/{c['id']}.{container}", "container": container,
                                       "sha256": hashlib.sha256(data).hexdigest(),
                                       "duration_ms": round(c["duration_s"] * 1000),
                                       "source": c["source"], "item": c["item"], "licence": c["licence"]},
                             "thresholds": {"max_wer": WER.get(category, .20),
                                            "max_latency_ms": 9000 if c["duration_s"] > 20 else 6000,
                                            "max_cost_usd": MAX_COST_USD}},
                   "rubric": ("Silence or noise only: the transcript is empty." if silent else
                              f"{c['category']}: the transcript matches the reference within the word-error-rate ceiling."),
                   "checks": checks}
            if forced:
                row["input"]["route"] = "fallback"
            print(json.dumps(row, ensure_ascii=False, separators=(",", ":")))


if __name__ == "__main__":
    main()
