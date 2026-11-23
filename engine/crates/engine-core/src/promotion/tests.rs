use super::*;
use serde_json::{Value, json};

const REV: &str = "b7979d57f81401aaabb21aba2cb82c64cddafd96";
const SHA: &str = "eecd8e63076797b780a1a5bb10ed83cc64ba249e788b8f149383dae4834cfb71";

fn task(passed: bool) -> Value {
    json!({"passed": passed, "critical_failure": false, "total_cases": 2, "score": if passed {1.0} else {0.5}, "threshold": 1.0})
}

fn report() -> Value {
    let mut tasks = serde_json::Map::new();
    for t in CANONICAL_TASKS
        .iter()
        .chain(["steer_screen", "screen_review"].iter())
    {
        tasks.insert((*t).into(), task(true));
    }
    json!({
        "dataset": "datamonsterr/duolify-golden", "dataset_revision": REV, "dataset_source": "repo",
        "dataset_verified": true, "dataset_sha256": SHA, "provider": "live", "status": "passed",
        "passed": true, "critical_failure": false, "budget_exceeded": false, "fingerprint": "fp",
        "dataset_cases": 16, "task_reports": tasks
    })
}

#[test]
fn a_sampled_run_cannot_promote_anything() {
    // Review F4: a sample that ran only the benign row of each screen must not promote screens.
    let mut r = report();
    r["dataset_cases"] = json!(20);
    assert!(matches!(
        gate(&r).check("steer_screen"),
        Err(Denial::EvidenceRejected(why)) if why.contains("sample")
    ));
    // Legacy evidence without the full-set count is rejected too.
    let mut r = report();
    r.as_object_mut().unwrap().remove("dataset_cases");
    assert!(matches!(
        gate(&r).check("course_preview"),
        Err(Denial::EvidenceRejected(_))
    ));
}

fn gate(v: &Value) -> Gate {
    Gate::from_report(&v.to_string(), "fp")
}

#[test]
fn passing_live_evidence_promotes_only_evaluated_tasks() {
    let g = gate(&report());
    assert_eq!(g.check("steer_screen"), Ok(()));
    assert_eq!(g.check("course_preview"), Ok(()));
    assert_eq!(g.check("character_create"), Err(Denial::TaskNotPromoted));
}

#[test]
fn a_failed_task_is_not_promoted() {
    let mut r = report();
    r["task_reports"]["steer_screen"] = task(false);
    assert_eq!(gate(&r).check("steer_screen"), Err(Denial::TaskNotPromoted));
    let mut r = report();
    r["task_reports"]["screen_review"]["critical_failure"] = json!(true);
    assert_eq!(
        gate(&r).check("screen_review"),
        Err(Denial::TaskNotPromoted)
    );
    let mut r = report();
    r["task_reports"]["screen_review"]["total_cases"] = json!(0);
    r["dataset_cases"] = json!(14);
    assert_eq!(
        gate(&r).check("screen_review"),
        Err(Denial::TaskNotPromoted)
    );
}

#[test]
fn missing_or_unparsable_evidence_fails_closed() {
    for raw in ["", "{}", "not json", "{\"status\":\"missing_secrets\"}"] {
        assert_eq!(
            Gate::from_report(raw, "fp").check("steer_screen"),
            Err(Denial::EvidenceMissing),
            "{raw}"
        );
    }
}

#[test]
fn stale_evidence_fails_closed() {
    let g = Gate::from_report(&report().to_string(), "other-build");
    assert_eq!(g.check("steer_screen"), Err(Denial::EvidenceStale));
    assert_eq!(Denial::EvidenceStale.code(), "promotion_evidence_stale");
}

#[test]
fn run_level_rule_rejects_non_promotable_reports() {
    let cases: Vec<(&str, Value)> = vec![
        ("provider", json!("mock")),
        ("status", json!("missing_secrets")),
        ("status", json!("budget_exceeded")),
        ("passed", json!(false)),
        ("critical_failure", json!(true)),
        ("budget_exceeded", json!(true)),
        ("dataset_source", json!("fixture")),
        ("dataset_source", json!("custom")),
        ("dataset_verified", json!(false)),
        ("dataset_revision", json!("main")),
        (
            "dataset_revision",
            json!("0123456789abcdef0123456789abcdef01234567"),
        ),
        ("dataset_sha256", json!("00")),
        ("dataset", json!("someone/else")),
    ];
    for (field, value) in cases {
        let mut r = report();
        r[field] = value.clone();
        let denial = gate(&r).check("steer_screen").unwrap_err();
        assert_eq!(
            denial.code(),
            "promotion_evidence_rejected",
            "{field}={value}"
        );
    }
    let mut r = report();
    r["task_reports"]["tutor_chat"] = task(false);
    assert_eq!(
        gate(&r).check("steer_screen").unwrap_err().code(),
        "promotion_evidence_rejected"
    );
}

#[test]
fn fingerprint_is_stable_and_hex() {
    let a = fingerprint();
    assert_eq!(a, fingerprint());
    assert_eq!(a.len(), 64);
}

#[test]
fn live_narration_evidence_binds_the_vendor_client() {
    // lesson_narration evidence is a live ElevenLabs run, so the client that shapes it is fingerprinted.
    assert!(
        SOURCES
            .iter()
            .any(|(p, _)| *p == "engine-providers/src/elevenlabs.rs")
    );
    assert!(!EXCLUDED_SOURCES.contains(&"engine-providers/src/elevenlabs.rs"));
}

#[test]
fn launch_tasks_include_every_canonical_task_once() {
    for task in CANONICAL_TASKS {
        assert!(LAUNCH_TASKS.contains(&task), "{task}");
    }
    let mut unique = LAUNCH_TASKS.to_vec();
    unique.sort_unstable();
    unique.dedup();
    assert_eq!(unique.len(), LAUNCH_TASKS.len());
    assert!(LAUNCH_TASKS.contains(&"lesson_narration"));
}

#[test]
fn launch_coverage_names_each_closed_task() {
    let gate = Gate::for_tasks(&CANONICAL_TASKS);
    let missing = gate.missing_launch_tasks();
    assert!(missing.contains(&"outline_replan"));
    assert!(missing.contains(&"lesson_narration"));
    assert!(!missing.contains(&"course_preview"));
    assert!(
        Gate::for_tasks(&LAUNCH_TASKS)
            .missing_launch_tasks()
            .is_empty()
    );
    // Invalid evidence closes every launch task.
    let gate = Gate::from_report("{}", &fingerprint());
    assert_eq!(gate.missing_launch_tasks(), LAUNCH_TASKS.to_vec());
}

/// Review F2: every non-Rust input the production crates embed (schemas, persona fixture, voice
/// table, route config), the server's prompt construction, the gRPC contract, the dependency lock
/// and the eval semantics that decide what "passed" means are fingerprinted.
#[test]
fn fingerprint_follows_embedded_inputs_contract_dependencies_and_eval_semantics() {
    let repo = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../../..");
    let listed = |rel: &str| SOURCES.iter().any(|(label, _)| rel.ends_with(label));
    for krate in ["engine-core", "engine-providers", "engine-server"] {
        let mut stack = vec![repo.join("engine/crates").join(krate).join("src")];
        while let Some(dir) = stack.pop() {
            for entry in std::fs::read_dir(&dir).unwrap() {
                let path = entry.unwrap().path();
                if path.is_dir() {
                    stack.push(path);
                    continue;
                }
                let name = path.file_name().unwrap().to_string_lossy().to_string();
                if !name.ends_with(".rs") || name == "tests.rs" || name.ends_with("_tests.rs") {
                    continue;
                }
                let text = std::fs::read_to_string(&path).unwrap();
                for chunk in text.split("include_str!(").skip(1) {
                    let Some(target) = chunk.split('"').nth(1) else {
                        continue;
                    };
                    let file = std::fs::canonicalize(path.parent().unwrap().join(target)).unwrap();
                    let rel = file
                        .strip_prefix(std::fs::canonicalize(&repo).unwrap())
                        .unwrap()
                        .to_string_lossy()
                        .replace('\\', "/");
                    if rel.ends_with("live-report.json") || rel.ends_with("pinned-datasets.toml") {
                        continue; // the evidence and its pins are checked, not fingerprinted
                    }
                    assert!(
                        listed(&rel),
                        "{} embeds {rel}, which is not fingerprinted",
                        path.display()
                    );
                }
            }
        }
    }
    for rel in [
        "engine/crates/engine-server/src/lib.rs",
        "engine/crates/engine-server/src/main.rs",
        "proto/duolify/engine/v1/engine.proto",
        "engine/Cargo.lock",
        "engine/crates/engine-eval/src/live.rs",
        "engine/crates/engine-eval/src/runner.rs",
        "engine/crates/engine-eval/src/scoring.rs",
        "engine/crates/engine-eval/src/transcribe.rs",
    ] {
        assert!(listed(rel), "{rel} must be fingerprinted");
    }
}

/// Every non-test source under engine-core, engine-providers and engine-eval is either fingerprinted or
/// explicitly excluded, so a new prompt module cannot escape the evidence.
#[test]
fn fingerprint_sources_cover_every_ai_module() {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("..");
    let mut found = vec![];
    for krate in ["engine-core", "engine-providers", "engine-eval"] {
        let mut stack = vec![root.join(krate).join("src")];
        while let Some(dir) = stack.pop() {
            for entry in std::fs::read_dir(&dir).unwrap() {
                let path = entry.unwrap().path();
                if path.is_dir() {
                    stack.push(path);
                } else if path.extension().is_some_and(|e| e == "rs") {
                    let name = path.file_name().unwrap().to_string_lossy().to_string();
                    if name == "tests.rs" || name.ends_with("_tests.rs") {
                        continue;
                    }
                    let rel = path
                        .strip_prefix(&root)
                        .unwrap()
                        .to_string_lossy()
                        .replace('\\', "/");
                    found.push(rel);
                }
            }
        }
    }
    for file in found {
        assert!(
            SOURCES.iter().any(|(p, _)| *p == file) || EXCLUDED_SOURCES.contains(&file.as_str()),
            "{file} must be listed in promotion::SOURCES or EXCLUDED_SOURCES"
        );
    }
}
