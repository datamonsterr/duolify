#!/usr/bin/env python3
"""Transcription model comparison on OpenRouter (voice-input spike, tasks 1.3).

Stdlib only. The key is read from OPENROUTER_API_KEY and never printed or written.

  run.py run    --audio DIR --models a/b,c/d [--containers webm,m4a] [--out results.json]
  run.py policy --audio DIR --models a/b,...          # data_collection=deny and zdr=true routability
  run.py report --results results.json                # markdown tables

`run` stops before the next request once the spend reaches --budget (USD).
"""
import argparse, base64, json, os, re, statistics, sys, time, unicodedata, urllib.error, urllib.request

URL = "https://openrouter.ai/api/v1/audio/transcriptions"
HERE = os.path.dirname(os.path.abspath(__file__))


def call(model, path, fmt, provider=None, extra=None, timeout=90):
    body = {"model": model, "input_audio": {"data": base64.b64encode(open(path, "rb").read()).decode(), "format": fmt}}
    body["provider"] = provider if provider is not None else {"data_collection": "deny"}
    if extra:
        body.update(extra)
    req = urllib.request.Request(
        URL, data=json.dumps(body).encode(),
        headers={"Authorization": "Bearer " + os.environ["OPENROUTER_API_KEY"], "Content-Type": "application/json",
                 "X-Title": "duolify-voice-spike"})
    t = time.perf_counter()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            data = json.loads(r.read().decode())
            return {"ok": True, "latency": time.perf_counter() - t, "data": data}
    except urllib.error.HTTPError as e:
        return {"ok": False, "latency": time.perf_counter() - t, "status": e.code, "error": e.read().decode()[:300]}
    except Exception as e:  # timeout, connection reset
        return {"ok": False, "latency": time.perf_counter() - t, "status": 0, "error": type(e).__name__}


def norm(s):
    """Lowercase, NFKC, drop punctuation and symbols, hyphen and apostrophe handling, collapse spaces."""
    s = unicodedata.normalize("NFKC", s or "").lower().replace("-", " ").replace("’", "'")
    s = s.replace("'", "")
    s = "".join(c for c in s if unicodedata.category(c)[0] not in "PS")
    return re.sub(r"\s+", " ", s).strip()


def wer(ref, hyp):
    r, h = norm(ref).split(), norm(hyp).split()
    if not r:
        return None
    d = list(range(len(h) + 1))
    for i in range(1, len(r) + 1):
        prev, d[0] = d[0], i
        for j in range(1, len(h) + 1):
            cur = d[j]
            d[j] = min(d[j] + 1, d[j - 1] + 1, prev + (r[i - 1] != h[j - 1]))
            prev = cur
    return d[len(h)] / len(r)


def load_manifest():
    return json.load(open(os.path.join(HERE, "manifest.json")))["clips"]


def cmd_run(a):
    clips = load_manifest()
    results = json.load(open(a.out)) if os.path.exists(a.out) else {"rows": []}
    spent = sum(r.get("cost") or 0 for r in results["rows"])
    done = {(r["model"], r["clip"], r["container"]) for r in results["rows"]}
    for model in a.models.split(","):
        for c in clips:
            for fmt in a.containers.split(","):
                if (model, c["id"], fmt) in done:
                    continue
                if spent >= a.budget:
                    print("budget reached", round(spent, 4)); json.dump(results, open(a.out, "w"), indent=1); return
                p = os.path.join(a.audio, f"{c['id']}.{fmt}")
                r = call(model, p, fmt)
                row = {"model": model, "clip": c["id"], "category": c["category"], "container": fmt, "latency": round(r["latency"], 3),
                       "ok": r["ok"], "bytes": os.path.getsize(p)}
                if r["ok"]:
                    d = r["data"]; u = d.get("usage") or {}
                    row.update(text=d.get("text", ""), cost=u.get("cost"), seconds=u.get("seconds"), usage=u)
                    row["wer"] = wer(c.get("ref") or "", d.get("text", "")) if c.get("ref") else None
                else:
                    row.update(status=r["status"], error=r["error"])
                spent += row.get("cost") or 0
                results["rows"].append(row)
                print(model, c["id"], fmt, "ok" if r["ok"] else f"ERR {r.get('status')}", row["latency"], row.get("cost"), flush=True)
        json.dump(results, open(a.out, "w"), indent=1)
    print("spent", round(spent, 5))


def cmd_policy(a):
    clip = next(c for c in load_manifest() if c["id"] == "en-clean-1")
    p = os.path.join(a.audio, "en-clean-1.webm")
    out = {}
    for model in a.models.split(","):
        out[model] = {}
        for name, prov in (("data_collection_deny", {"data_collection": "deny"}), ("zdr", {"zdr": True})):
            r = call(model, p, "webm", provider=prov)
            out[model][name] = "ok" if r["ok"] else f"{r['status']} {r['error'][:110]}"
            out[model][name + "_cost"] = (r["data"].get("usage") or {}).get("cost") if r["ok"] else None
    json.dump(out, open(a.out, "w"), indent=1); print(json.dumps(out, indent=1))


def pct(xs, q):
    xs = sorted(xs)
    return xs[min(len(xs) - 1, int(round(q * (len(xs) - 1))))] if xs else float("nan")


def cmd_report(a):
    rows = json.load(open(a.results))["rows"]
    models = list(dict.fromkeys(r["model"] for r in rows))
    dur = {c["id"]: c["duration_s"] for c in load_manifest()}
    cats = list(dict.fromkeys(r["category"] for r in rows if r["category"] not in ("silence", "noise_only")))
    print("| Model | " + " | ".join(cats) + " | all speech | fail |\n|---|" + "---|" * (len(cats) + 2))
    for m in models:
        mr = [r for r in rows if r["model"] == m]
        cells = []
        for c in cats:
            ws = [r["wer"] for r in mr if r["category"] == c and r.get("wer") is not None]
            cells.append(f"{100 * statistics.mean(ws):.1f}%" if ws else "-")
        allw = [r["wer"] for r in mr if r["category"] in cats and r.get("wer") is not None]
        fails = sum(1 for r in mr if not r["ok"])
        print(f"| `{m}` | " + " | ".join(cells) + f" | {100 * statistics.mean(allw):.1f}% | {fails} |")
    print("\n| Model | silence + noise-only clips: words returned | latency p50 / p95 (s) | cost per clip (USD) | cost per 30 s (USD) |\n|---|---|---|---|---|")
    for m in models:
        mr = [r for r in rows if r["model"] == m and r["ok"]]
        sil = [f"{r['clip']}/{r['container']}: {len(norm(r['text']).split())}" for r in mr if r["category"] in ("silence", "noise_only")]
        lat = [r["latency"] for r in mr]
        cost = [r["cost"] for r in mr if r.get("cost") is not None]
        secs = sum(dur[r["clip"]] for r in mr if r.get("cost") is not None)
        per30 = (sum(cost) / secs * 30) if secs else float("nan")
        print(f"| `{m}` | {', '.join(sil)} | {pct(lat, .5):.2f} / {pct(lat, .95):.2f} | {statistics.mean(cost):.6f} | {per30:.5f} |")
    print("\n| Model | WER webm | WER m4a |\n|---|---|---|")
    for m in models:
        cells = []
        for f in ("webm", "m4a"):
            ws = [r["wer"] for r in rows if r["model"] == m and r["container"] == f and r.get("wer") is not None]
            cells.append(f"{100 * statistics.mean(ws):.1f}%" if ws else "-")
        print(f"| `{m}` | {cells[0]} | {cells[1]} |")
    print("\nTotal spend (USD):", round(sum(r.get("cost") or 0 for r in rows), 5))


if __name__ == "__main__":
    ap = argparse.ArgumentParser(); sp = ap.add_subparsers(dest="cmd", required=True)
    r = sp.add_parser("run"); r.add_argument("--audio", required=True); r.add_argument("--models", required=True)
    r.add_argument("--containers", default="webm,m4a"); r.add_argument("--out", default="results.json"); r.add_argument("--budget", type=float, default=0.5)
    pol = sp.add_parser("policy"); pol.add_argument("--audio", required=True); pol.add_argument("--models", required=True); pol.add_argument("--out", default="policy.json")
    rep = sp.add_parser("report"); rep.add_argument("--results", required=True)
    a = ap.parse_args()
    {"run": cmd_run, "policy": cmd_policy, "report": cmd_report}[a.cmd](a)
