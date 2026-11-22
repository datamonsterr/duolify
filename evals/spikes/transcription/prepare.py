#!/usr/bin/env python3
"""Rebuild the spike's audio set under OUT (default ./audio). Nothing here is committed.

Needs: python with pyarrow, ffmpeg, the `hf` CLI logged in (public datasets only; never print the token).
  uv run --with pyarrow python prepare.py --out audio [--tts]

Downloads (about 1.2 GB, once): LibriSpeech test.clean shard 0, GLOBE_V2 test shard 1, FLEURS vi_vn test audio.
--tts adds four synthetic technical-vocabulary clips from ElevenLabs (346 characters in total); it reads
ELEVENLABS_API_KEY and ELEVENLABS_VOICE_ID from ~/.config/duolify/secrets.env and prints neither.
Selection is deterministic, so the ids and reference texts match manifest.json.
"""
import argparse, csv, io, json, os, subprocess, tarfile, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
TECH = ["Declare the variable with const, then return a closure that captures it.",
        "A promise resolves asynchronously, so await it inside an async function before you read the result.",
        "Our roadmap for next quarter lists three OKRs, and the product manager will prioritise the backlog.",
        "Run the sprint retrospective, then update the roadmap and the OKR scorecard."]


def hf(repo, *files, local):
    subprocess.run(["hf", "download", repo, *files, "--repo-type", "dataset", "--local-dir", local], check=True, stdout=subprocess.DEVNULL)


def ff(*args):
    subprocess.run(["ffmpeg", "-v", "error", "-y", *args], check=True)


def main():
    ap = argparse.ArgumentParser(); ap.add_argument("--out", default="audio"); ap.add_argument("--tts", action="store_true")
    a = ap.parse_args(); raw = os.path.join(a.out, "_raw"); wav = os.path.join(a.out, "_wav")
    os.makedirs(raw, exist_ok=True); os.makedirs(wav, exist_ok=True)
    import pyarrow.parquet as pq
    # LibriSpeech (CC-BY-4.0): first three 12-30 word utterances of row group 3
    hf("openslr/librispeech_asr", "all/test.clean/0000.parquet", local=raw + "/ls")
    n = 0
    for r in pq.ParquetFile(raw + "/ls/all/test.clean/0000.parquet").read_row_group(3).to_pylist():
        if 12 <= len(r["text"].split()) <= 30 and n < 3:
            n += 1; open(f"{raw}/ls{n}.flac", "wb").write(r["audio"]["bytes"]); ff("-i", f"{raw}/ls{n}.flac", "-ar", "16000", "-ac", "1", f"{wav}/en-clean-{n}.wav")
    # GLOBE_V2 (CC0-1.0): first 5-10 s clip of each of the first accents found, in file order
    hf("MushanW/GLOBE_V2", "data/test-00001-of-00002.parquet", local=raw + "/globe")
    p = pq.ParquetFile(raw + "/globe/data/test-00001-of-00002.parquet"); seen = {}
    for g in range(p.metadata.num_row_groups):
        for r in p.read_row_group(g).to_pylist():
            acc = r["accent"]
            if acc in ("Indian English", "Scottish English", "South African English", "Nigerian English", "Singaporean English", "Hong Kong English") \
                    and acc not in seen and 5 <= r["duration"] <= 10 and len(r["transcript"].split()) >= 10:
                seen[acc] = r
        if len(seen) >= 3: break
    for i, r in enumerate(list(seen.values())[:3], 1):
        open(f"{raw}/ac{i}.bin", "wb").write(r["audio"]["bytes"]); ff("-i", f"{raw}/ac{i}.bin", "-ar", "16000", "-ac", "1", f"{wav}/en-accent-{i}.wav")
    # FLEURS vi_vn (CC-BY-4.0): first three distinct sentences of 6-12 s in test.tsv order
    hf("google/fleurs", "data/vi_vn/test.tsv", "data/vi_vn/audio/test.tar.gz", local=raw + "/fleurs")
    rows = list(csv.reader(open(raw + "/fleurs/data/vi_vn/test.tsv", encoding="utf8"), delimiter="\t")); pick = []; seen_t = set()
    for r in rows:
        if 6 <= int(r[-2]) / 16000 <= 12 and r[3] not in seen_t and len(pick) < 3:
            seen_t.add(r[3]); pick.append(r[1])
    with tarfile.open(raw + "/fleurs/data/vi_vn/audio/test.tar.gz") as t:
        for m in t:
            if m.name.split("/")[-1] in pick:
                i = pick.index(m.name.split("/")[-1]) + 1; open(f"{raw}/vi{i}.wav", "wb").write(t.extractfile(m).read())
    for i in (1, 2, 3):
        ff("-i", f"{raw}/vi{i}.wav", "-ar", "16000", "-ac", "1", f"{wav}/vi-{i}.wav")
    # generated: noise mixes, silence, noise-only
    ff("-i", f"{raw}/ls2.flac", "-f", "lavfi", "-i", "anoisesrc=color=pink:amplitude=0.08:sample_rate=16000:duration=20",
       "-filter_complex", "[0:a][1:a]amix=inputs=2:duration=first:normalize=0", "-ar", "16000", "-ac", "1", f"{wav}/en-noisy-1.wav")
    ff("-i", f"{raw}/ls3.flac", "-f", "lavfi", "-i", "anoisesrc=color=brown:amplitude=0.15:sample_rate=16000:duration=20",
       "-filter_complex", "[0:a][1:a]amix=inputs=2:duration=first:normalize=0", "-ar", "16000", "-ac", "1", f"{wav}/en-noisy-2.wav")
    ff("-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono", "-t", "5", f"{wav}/silence-1.wav")
    ff("-f", "lavfi", "-i", "anoisesrc=color=pink:amplitude=0.02:sample_rate=16000:duration=6", "-ar", "16000", "-ac", "1", f"{wav}/noise-only-1.wav")
    if a.tts:
        env = {}
        for line in open(os.path.expanduser("~/.config/duolify/secrets.env")):
            if line.startswith(("ELEVENLABS_API_KEY=", "ELEVENLABS_VOICE_ID=")):
                k, v = line.strip().split("=", 1); env[k] = v.strip("\"'")
        assert sum(map(len, TECH)) <= 600
        for i, s in enumerate(TECH, 1):
            req = urllib.request.Request(f"https://api.elevenlabs.io/v1/text-to-speech/{env['ELEVENLABS_VOICE_ID']}?output_format=mp3_44100_64",
                                         data=json.dumps({"text": s, "model_id": "eleven_multilingual_v2"}).encode(),
                                         headers={"xi-api-key": env["ELEVENLABS_API_KEY"], "content-type": "application/json"})
            open(f"{raw}/tech{i}.mp3", "wb").write(urllib.request.urlopen(req, timeout=60).read())
            ff("-i", f"{raw}/tech{i}.mp3", "-ar", "16000", "-ac", "1", f"{wav}/tech-{i}.wav")
    # the two containers the app will send: WebM/Opus and MP4/AAC (m4a)
    for f in sorted(os.listdir(wav)):
        b = f[:-4]
        ff("-i", f"{wav}/{f}", "-c:a", "libopus", "-b:a", "24k", "-application", "voip", "-ar", "48000", f"{a.out}/{b}.webm")
        ff("-i", f"{wav}/{f}", "-c:a", "aac", "-b:a", "32k", "-ar", "16000", "-movflags", "+faststart", f"{a.out}/{b}.m4a")
    print("clips:", len(os.listdir(wav)), "->", a.out)


if __name__ == "__main__":
    main()
