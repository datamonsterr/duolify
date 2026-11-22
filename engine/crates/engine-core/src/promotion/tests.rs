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
        "task_reports": tasks
    })
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

/// Every non-test source under engine-core and engine-providers is either fingerprinted or
/// explicitly excluded, so a new prompt module cannot escape the evidence.
#[test]
fn fingerprint_sources_cover_every_ai_module() {
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("..");
    let mut found = vec![];
    for krate in ["engine-core", "engine-providers"] {
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
