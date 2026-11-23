//! E10-06 e2e (eval CLI process): recording promotion evidence for a deployed engine.
//! `promote` accepts only a live, pinned, verified, passing report of this build; `--data
//! <file> --revision <rev>` runs a local copy of a pinned revision only when its bytes match the pin.
mod common;

use serde_json::{Value, json};
use std::path::{Path, PathBuf};
use std::process::Command;

const REV: &str = "d902ce8cfacc2abf8d585930e2a15d471515b07a";
const SHA: &str = "202ac078afc07c9adf1f74eb289c458215088acb005a12538c6051abb7e79ed5";

fn temp(name: &str) -> PathBuf {
    let dir = std::env::temp_dir().join(format!(
        "eval-promote-{name}-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&dir).unwrap();
    dir
}

fn golden_path() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../../../evals/datasets/duolify-golden/golden.jsonl")
}

fn live_report(fingerprint: &str) -> Value {
    let mut tasks = serde_json::Map::new();
    for t in engine_eval::CANONICAL_TASKS
        .iter()
        .chain(["steer_screen", "screen_review"].iter())
    {
        tasks.insert(
            (*t).into(),
            json!({"task": t, "model": "tencent/hy4-flash", "score": 1.0, "threshold": 1.0,
            "passed": true, "critical_failure": false, "total_cases": 1, "passed_cases": 1,
            "deterministic_score": 1.0, "judge_score": null}),
        );
    }
    json!({
        "timestamp_millis": 1, "dataset": "datamonsterr/duolify-golden", "dataset_revision": REV,
        "dataset_source": "repo", "dataset_verified": true, "dataset_sha256": SHA,
        "fingerprint": fingerprint, "dataset_cases": 1, "prompt_revision": "test", "provider": "live",
        "budget_usd": 0.5, "total_cost_usd": 0.002, "budget_exceeded": false, "status": "passed",
        "live_promoted": true, "passed": true, "critical_failure": false,
        "task_reports": tasks, "results": []
    })
}

fn promote(report: &Value, dir: &Path) -> (std::process::Output, PathBuf) {
    let input = dir.join("report.json");
    let out = dir.join("evidence.json");
    std::fs::write(&input, report.to_string()).unwrap();
    let output = Command::new(common::eval_bin())
        .arg("promote")
        .arg(&input)
        .arg(&out)
        .output()
        .unwrap();
    (output, out)
}

#[test]
fn promote_records_a_passing_live_report_of_this_build_as_engine_evidence() {
    let dir = temp("ok");
    let fp = engine_core::promotion::fingerprint();
    let (output, out) = promote(&live_report(&fp), &dir);
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    let evidence = std::fs::read_to_string(&out).unwrap();
    let gate = engine_core::promotion::Gate::from_report(&evidence, &fp);
    assert_eq!(gate.check("steer_screen"), Ok(()));
    assert!(gate.check("character_create").is_err());
    let _ = std::fs::remove_dir_all(dir);
}

#[test]
fn promote_refuses_mock_missing_secret_failed_and_foreign_build_reports() {
    let fp = engine_core::promotion::fingerprint();
    let mut cases: Vec<(&str, Value)> = vec![];
    let mut r = live_report(&fp);
    r["provider"] = json!("mock");
    cases.push(("mock", r));
    let mut r = live_report(&fp);
    r["status"] = json!("missing_secrets");
    r["provider"] = json!("none");
    cases.push(("missing_secrets", r));
    let mut r = live_report(&fp);
    r["critical_failure"] = json!(true);
    cases.push(("critical", r));
    let mut r = live_report(&fp);
    r["dataset_source"] = json!("custom");
    r["dataset_verified"] = json!(false);
    cases.push(("custom data", r));
    cases.push(("other build", live_report("0".repeat(64).as_str())));
    for (name, report) in cases {
        let dir = temp("refuse");
        let (output, out) = promote(&report, &dir);
        assert!(!output.status.success(), "{name} must be refused");
        assert!(!out.exists(), "{name} must not write evidence");
        let _ = std::fs::remove_dir_all(dir);
    }
}

/// Synthetic pinned double (`duolify-fixtures/e10-04` in pinned-datasets.toml).
const FIXTURE_DATASET: &str = r#"{"id":"preview-1","task":"course_preview","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
{"id":"outline-1","task":"course_outline","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
{"id":"lesson-1","task":"lesson_slides","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
{"id":"test-1","task":"lesson_test","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
{"id":"chat-1","task":"tutor_chat","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
{"id":"note-1","task":"note_enrich","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
"#;
const FIXTURE_REV: &str = "0123456789abcdef0123456789abcdef01234567";
const FIXTURE_SHA: &str = "e5bcda9d5a4b84a383f24271af9f763ffb38dc51625ec2e1a00daa3feb947f89";

#[test]
fn pinned_local_copy_is_verified_and_recorded_as_repo_source() {
    let dir = temp("pinned");
    let data = dir.join("golden.jsonl");
    std::fs::write(&data, FIXTURE_DATASET).unwrap();
    let output = Command::new(common::eval_bin())
        .args(["--provider", "mock", "--data"])
        .arg(&data)
        .args([
            "--dataset",
            "duolify-fixtures/e10-04",
            "--revision",
            FIXTURE_REV,
            "--reports",
        ])
        .arg(dir.join("reports"))
        .env_remove("EVAL_SAMPLE_LIMIT")
        .env_remove("EVAL_BUDGET_USD")
        .output()
        .unwrap();
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(
        output.status.success(),
        "{stdout} {}",
        String::from_utf8_lossy(&output.stderr)
    );
    assert!(stdout.contains("verified from"), "{stdout}");
    let report = std::fs::read_dir(dir.join("reports"))
        .unwrap()
        .map(|e| e.unwrap().path())
        .find(|p| p.extension().is_some_and(|e| e == "json"))
        .expect("report written");
    let v: Value = serde_json::from_str(&std::fs::read_to_string(report).unwrap()).unwrap();
    assert_eq!(v["dataset_source"], "repo");
    assert_eq!(v["dataset_verified"], true);
    assert_eq!(v["dataset_revision"], FIXTURE_REV);
    assert_eq!(v["dataset_sha256"], FIXTURE_SHA);
    assert_eq!(v["fingerprint"], engine_core::promotion::fingerprint());
    // A mock run is never promotable, whatever its dataset.
    assert_eq!(v["live_promoted"], false);
    let _ = std::fs::remove_dir_all(dir);
}

#[test]
fn a_tampered_local_copy_is_refused_before_any_case_runs() {
    let dir = temp("tampered");
    let data = dir.join("golden.jsonl");
    let mut bytes = std::fs::read(golden_path()).unwrap();
    bytes.extend_from_slice(b"\n");
    std::fs::write(&data, bytes).unwrap();
    let output = Command::new(common::eval_bin())
        .args(["--provider", "mock", "--data"])
        .arg(&data)
        .args(["--revision", REV, "--reports"])
        .arg(dir.join("reports"))
        .output()
        .unwrap();
    assert!(!output.status.success());
    assert!(String::from_utf8_lossy(&output.stderr).contains("integrity failure"));
    assert!(!dir.join("reports").exists());
    let _ = std::fs::remove_dir_all(dir);
}

#[test]
fn evidence_subcommand_reports_the_embedded_state() {
    let output = Command::new(common::eval_bin())
        .arg("evidence")
        .output()
        .unwrap();
    assert!(output.status.success());
    assert!(String::from_utf8_lossy(&output.stdout).contains("promotion evidence"));
}

#[test]
fn require_launch_fails_exactly_when_committed_evidence_leaves_a_launch_task_closed() {
    let output = Command::new(common::eval_bin())
        .args(["evidence", "--require-launch"])
        .output()
        .unwrap();
    let stdout = String::from_utf8_lossy(&output.stdout);
    let covered = engine_core::promotion::Gate::embedded()
        .missing_launch_tasks()
        .is_empty();
    assert_eq!(output.status.success(), covered, "{stdout}");
    if !covered {
        assert!(stdout.contains("::error::"), "{stdout}");
        assert!(stdout.contains("mise run engine:eval-golden"), "{stdout}");
        assert!(stdout.contains("mise run engine:promote"), "{stdout}");
    }
}

#[test]
fn launch_evidence_rejects_stale_and_partial_evidence_with_the_refresh_commands() {
    use engine_core::promotion::{Gate, LAUNCH_TASKS};
    // Stale: a valid report of another build.
    let stale = Gate::from_report(
        &live_report("0".repeat(64).as_str()).to_string(),
        &engine_core::promotion::fingerprint(),
    );
    let err = engine_eval::launch_evidence(&stale).unwrap_err();
    assert!(err.contains("promotion_evidence_stale"), "{err}");
    assert!(err.contains("mise run engine:eval-golden"), "{err}");
    assert!(err.contains("mise run engine:promote"), "{err}");
    // Partial: valid for this build, but launch tasks without live cases stay closed.
    let fp = engine_core::promotion::fingerprint();
    let partial = Gate::from_report(&live_report(&fp).to_string(), &fp);
    let err = engine_eval::launch_evidence(&partial).unwrap_err();
    assert!(
        err.contains("lesson_narration") && err.contains("outline_replan"),
        "{err}"
    );
    // Complete.
    assert!(engine_eval::launch_evidence(&Gate::for_tasks(&LAUNCH_TASKS)).is_ok());
}
