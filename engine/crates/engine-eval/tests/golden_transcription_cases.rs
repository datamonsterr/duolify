//! The reviewed live set carries the transcription cases the spec asks for (voice-input,
//! immersive-mobile-lesson-player 6.7): languages, accents, noise, silence and technical terms,
//! each with word-error-rate, latency and cost thresholds and a pinned recording digest.
use serde_json::Value;
use std::collections::BTreeSet;
use std::path::Path;

fn live_rows() -> Vec<Value> {
    let path = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../../../evals/datasets/duolify-golden/golden.jsonl");
    std::fs::read_to_string(path)
        .unwrap()
        .lines()
        .filter(|l| !l.trim().is_empty())
        .map(|l| serde_json::from_str::<Value>(l).unwrap())
        .filter(|r| r["task"] == "transcribe")
        .collect()
}

#[test]
fn the_live_set_covers_every_required_category_and_both_containers() {
    let rows = live_rows();
    let categories: BTreeSet<&str> = rows
        .iter()
        .map(|r| r["input"]["category"].as_str().unwrap())
        .collect();
    for needed in [
        "clean_en",
        "accented_en",
        "noisy_en",
        "technical_en",
        "vietnamese",
        "spanish",
        "silence",
        "noise_only",
        "long_en",
        "short_en",
    ] {
        assert!(categories.contains(needed), "missing category {needed}");
    }
    let containers: BTreeSet<&str> = rows
        .iter()
        .map(|r| r["input"]["audio"]["container"].as_str().unwrap())
        .collect();
    assert_eq!(containers, BTreeSet::from(["m4a", "webm"]));
    assert!(
        rows.iter().any(|r| r["input"]["route"] == "fallback"),
        "the fallback model is measured on its own"
    );
}

#[test]
fn every_row_pins_its_audio_and_states_its_thresholds() {
    let rows = live_rows();
    assert!(rows.len() >= 40);
    let mut ids = BTreeSet::new();
    for row in &rows {
        let id = row["id"].as_str().unwrap();
        assert!(ids.insert(id.to_string()), "duplicate id {id}");
        let audio = &row["input"]["audio"];
        let sha = audio["sha256"].as_str().unwrap();
        assert_eq!(sha.len(), 64, "{id}");
        assert!(sha.bytes().all(|b| b.is_ascii_hexdigit()), "{id}");
        assert!(
            audio["file"].as_str().unwrap().starts_with("audio/"),
            "{id}"
        );
        assert!(
            audio["licence"].as_str().is_some_and(|s| !s.is_empty()),
            "{id}"
        );
        assert!(
            audio["source"].as_str().is_some_and(|s| !s.is_empty()),
            "{id}"
        );
        let ms = audio["duration_ms"].as_u64().unwrap();
        assert!((500..=30_000).contains(&ms), "{id}: {ms} ms");
        let t = &row["input"]["thresholds"];
        assert!(
            t["max_wer"].as_f64().unwrap() > 0.0 && t["max_wer"].as_f64().unwrap() <= 0.5,
            "{id}"
        );
        assert!(t["max_latency_ms"].as_u64().unwrap() <= 9000, "{id}");
        assert!(t["max_cost_usd"].as_f64().unwrap() <= 0.0005, "{id}");
        let silent = row["input"]["reference"].as_str().unwrap().is_empty();
        let checked: BTreeSet<String> = row["checks"]
            .as_array()
            .unwrap()
            .iter()
            .map(|c| {
                let spec: Value = serde_json::from_str(c["value"].as_str().unwrap()).unwrap();
                assert_eq!(c["critical"], true, "{id}");
                spec["path"].as_str().unwrap().to_string()
            })
            .collect();
        let expected: BTreeSet<String> = [
            "delivered",
            if silent { "silence_ok" } else { "wer_within" },
            "latency_within",
            "cost_within",
        ]
        .map(String::from)
        .into();
        assert_eq!(checked, expected, "{id}");
    }
}

#[test]
fn silence_and_noise_cases_expect_empty_text_on_both_models() {
    let rows = live_rows();
    for category in ["silence", "noise_only"] {
        let of: Vec<&Value> = rows
            .iter()
            .filter(|r| r["input"]["category"] == category)
            .collect();
        assert!(
            of.iter().any(|r| r["input"]["route"] != "fallback"),
            "{category} primary"
        );
        assert!(
            of.iter().any(|r| r["input"]["route"] == "fallback"),
            "{category} fallback"
        );
        for r in of {
            assert_eq!(r["input"]["reference"], "");
        }
    }
}
