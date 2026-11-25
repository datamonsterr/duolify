pub mod analytics;
pub mod compare;
pub mod judge;
pub mod live;
pub mod report;
pub mod runner;
pub mod scoring;
pub mod tasks;
pub mod transcribe;

pub use analytics::{SYSTEM_OPERATIONAL_UUID, emit_eval_run_completed};
pub use judge::parse_judge_verdict;
pub use report::{generate_json_report, generate_markdown_report};
pub use runner::{
    FetchedDataset, Runner, budget_from_args, compute_sha256, fetch_dataset,
    fetch_dataset_with_mirror, is_pinned_commit, mode_from_args, sample_limit_from_args,
    select_cases, verify_pinned_file,
};
pub use scoring::{
    Case, CaseResult, Check, EvalReport, TaskReport, Thresholds, aggregate_task_reports,
    check_promotion_gate, is_promotable,
};
pub use tasks::{CANONICAL_TASKS, TaskRegistry, canonical_task_name, model_route_for_task};

/// How to refresh `evals/promotion/live-report.json` (evals/README.md, "Refreshing promotion evidence").
pub const REFRESH_EVIDENCE: &str = "refresh it on this commit: mise run engine:eval-golden (needs OPENROUTER_API_KEY, ELEVENLABS_API_KEY and ELEVENLABS_VOICE_ID; about $0.003), then mise run engine:promote ../evals/reports/latest.json, then mise run engine:evidence -- --require-launch, and commit evals/promotion/live-report.json";

/// Verdict on committed promotion evidence for a build: `Ok` when it is valid and promotes every
/// launch task, otherwise `Err` naming the reason, the closed tasks and the refresh commands.
pub fn launch_evidence(gate: &engine_core::promotion::Gate) -> Result<String, String> {
    let missing = gate.missing_launch_tasks();
    match gate.summary() {
        Ok(s) if missing.is_empty() => Ok(format!(
            "promotion evidence: valid for this build ({}@{}); every launch task is promoted: {:?}",
            s.dataset, s.dataset_revision, s.passed_tasks
        )),
        Ok(s) => Err(format!(
            "promotion evidence ({}@{}) leaves launch tasks closed on a live engine: {missing:?}; add passing live golden cases for them and {REFRESH_EVIDENCE}",
            s.dataset, s.dataset_revision
        )),
        Err(d) => Err(format!(
            "promotion evidence is {} ({}): every live AI feature fails closed in a deployed engine built from this commit; {REFRESH_EVIDENCE}",
            d.code(),
            d.detail()
        )),
    }
}

pub mod calibration;
pub mod rubric;
