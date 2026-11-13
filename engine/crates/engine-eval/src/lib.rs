pub mod analytics;
pub mod judge;
pub mod report;
pub mod runner;
pub mod scoring;
pub mod tasks;

pub use analytics::{SYSTEM_OPERATIONAL_UUID, emit_eval_run_completed};
pub use judge::parse_judge_verdict;
pub use report::{generate_json_report, generate_markdown_report};
pub use runner::{
    FetchedDataset, Runner, budget_from_args, compute_sha256, fetch_dataset,
    fetch_dataset_with_mirror, is_pinned_commit, mode_from_args, sample_limit_from_args,
    select_cases,
};
pub use scoring::{
    Case, CaseResult, Check, EvalReport, TaskReport, Thresholds, aggregate_task_reports,
    check_promotion_gate, is_promotable,
};
pub use tasks::{CANONICAL_TASKS, TaskRegistry, canonical_task_name, model_route_for_task};
