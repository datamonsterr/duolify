//! E10-06 live promotion gate for a deployed engine.
//!
//! A model-backed feature runs on a live (non-fixture) route only when the committed live eval
//! report (`evals/promotion/live-report.json`) passes the promotion rule for its task: a live
//! run on a pinned, digest-verified Golden set revision, no critical failure, inside budget,
//! every canonical task passing, and the feature's own task passing with at least one case.
//! The report also carries the fingerprint of what it evaluated (route config, thresholds and
//! the prompt/validator/provider sources). Any change to those makes the evidence stale and the
//! feature fails closed until a new live run is recorded. No environment variable can enable a
//! feature: evidence is embedded at build time.
use serde::Deserialize;
use std::collections::{BTreeMap, HashMap};

/// The committed evidence, embedded at build time. `{}`-like placeholders mean "no evidence".
pub const EVIDENCE: &str = include_str!("../../../../../evals/promotion/live-report.json");
const PINS: &str = include_str!("../../../../../evals/pinned-datasets.toml");

/// Tasks the eval gate requires in every promotable run (engine-eval `CANONICAL_TASKS`).
pub const CANONICAL_TASKS: [&str; 6] = [
    "course_preview",
    "course_outline",
    "lesson_slides",
    "lesson_test",
    "tutor_chat",
    "note_enrich",
];

/// Every launch AI feature's task: the engine-server `GATED` tasks plus `lesson_narration`.
/// The ready pipeline fails when committed evidence does not promote all of them
/// (`duolify-eval evidence --require-launch`); engine-server tests keep the lists in step.
pub const LAUNCH_TASKS: [&str; 17] = [
    "course_preview",
    "course_outline",
    "lesson_slides",
    "lesson_test",
    "tutor_chat",
    "note_enrich",
    "setup_chat",
    "course_brief_chat",
    "outline_chat_change",
    "outline_replan",
    "steer_screen",
    "character_screen",
    "course_screen",
    "screen_review",
    "character_create",
    "lesson_narration",
    "transcribe",
];

/// Inputs that decide what a live run evaluated. Any byte change makes recorded evidence stale.
/// `fingerprint_sources_cover_every_ai_module` keeps this list complete.
pub const SOURCES: &[(&str, &str)] = &[
    (
        "engine/config/models.toml",
        include_str!("../../../../config/models.toml"),
    ),
    (
        "evals/thresholds.toml",
        include_str!("../../../../../evals/thresholds.toml"),
    ),
    // The closed CharacterSpec schema `character_create` maps into (characters::SCHEMA).
    (
        "services/api/internal/characters/schema/v1.json",
        include_str!("../../../../../services/api/internal/characters/schema/v1.json"),
    ),
    // The persona that replaces an invalid or suppressed Character (characters::publication_runtime).
    (
        "services/api/internal/characters/schema/original.fixture.json",
        include_str!(
            "../../../../../services/api/internal/characters/schema/original.fixture.json"
        ),
    ),
    // The gRPC contract whose Debug-rendered requests shape lesson prompts.
    (
        "proto/duolify/engine/v1/engine.proto",
        include_str!("../../../../../proto/duolify/engine/v1/engine.proto"),
    ),
    // Dependency versions (HTTP client, serde, prost) that shape requests and parsing.
    ("engine/Cargo.lock", include_str!("../../../../Cargo.lock")),
    // Server-side prompt and request construction (lesson context, Character segments).
    (
        "engine-server/src/lib.rs",
        include_str!("../../../engine-server/src/lib.rs"),
    ),
    (
        "engine-server/src/main.rs",
        include_str!("../../../engine-server/src/main.rs"),
    ),
    // The eval semantics that decide what a passing live case means.
    (
        "engine-eval/src/live.rs",
        include_str!("../../../engine-eval/src/live.rs"),
    ),
    (
        "engine-eval/src/runner.rs",
        include_str!("../../../engine-eval/src/runner.rs"),
    ),
    (
        "engine-eval/src/scoring.rs",
        include_str!("../../../engine-eval/src/scoring.rs"),
    ),
    (
        "engine-eval/src/transcribe.rs",
        include_str!("../../../engine-eval/src/transcribe.rs"),
    ),
    (
        "engine-eval/src/tasks.rs",
        include_str!("../../../engine-eval/src/tasks.rs"),
    ),
    (
        "engine-eval/src/judge.rs",
        include_str!("../../../engine-eval/src/judge.rs"),
    ),
    (
        "engine-core/src/narration/voices.json",
        include_str!("../narration/voices.json"),
    ),
    ("engine-core/src/lib.rs", include_str!("../lib.rs")),
    (
        "engine-core/src/transcribe/mod.rs",
        include_str!("../transcribe/mod.rs"),
    ),
    (
        "engine-core/src/structured.rs",
        include_str!("../structured.rs"),
    ),
    ("engine-core/src/slide.rs", include_str!("../slide.rs")),
    // Slide region contract: slots, limits, prompt text, schema and validators.
    (
        "engine/config/regions.toml",
        include_str!("../../../../config/regions.toml"),
    ),
    ("engine-core/src/regions.rs", include_str!("../regions.rs")),
    // Input guard (ADR 0025): pre-filter, verdict contract and per-surface failure rules.
    (
        "engine-core/src/guard/mod.rs",
        include_str!("../guard/mod.rs"),
    ),
    (
        "engine-core/src/guard/classifier.rs",
        include_str!("../guard/classifier.rs"),
    ),
    // Skill router (ADR 0025): decides which skills a routed prompt loads.
    (
        "engine-core/src/router/mod.rs",
        include_str!("../router/mod.rs"),
    ),
    (
        "engine-providers/src/decisions.rs",
        include_str!("../../../engine-providers/src/decisions.rs"),
    ),
    // Prompt skills (ADR 0025): the loader and every skill file shape what the model is asked.
    (
        "engine-core/src/skills/mod.rs",
        include_str!("../skills/mod.rs"),
    ),
    (
        "engine/config/skills/lesson_slides.toml",
        include_str!("../../../../config/skills/lesson_slides.toml"),
    ),
    (
        "engine/config/skills/tutor_chat.toml",
        include_str!("../../../../config/skills/tutor_chat.toml"),
    ),
    (
        "engine/config/skills/course_preview.toml",
        include_str!("../../../../config/skills/course_preview.toml"),
    ),
    (
        "engine/config/skills/outline_replan.toml",
        include_str!("../../../../config/skills/outline_replan.toml"),
    ),
    (
        "engine/config/skills/course_outline.toml",
        include_str!("../../../../config/skills/course_outline.toml"),
    ),
    (
        "engine/config/skills/outline_chat_change.toml",
        include_str!("../../../../config/skills/outline_chat_change.toml"),
    ),
    (
        "engine-core/src/courses/routed.rs",
        include_str!("../courses/routed.rs"),
    ),
    (
        "engine-core/src/accounts/mod.rs",
        include_str!("../accounts/mod.rs"),
    ),
    (
        "engine-core/src/characters/mod.rs",
        include_str!("../characters/mod.rs"),
    ),
    (
        "engine-core/src/characters/narrate.rs",
        include_str!("../characters/narrate.rs"),
    ),
    (
        "engine-core/src/chat/mod.rs",
        include_str!("../chat/mod.rs"),
    ),
    (
        "engine-core/src/chat/stream.rs",
        include_str!("../chat/stream.rs"),
    ),
    (
        "engine-core/src/courses/mod.rs",
        include_str!("../courses/mod.rs"),
    ),
    (
        "engine-core/src/courses/outline.rs",
        include_str!("../courses/outline.rs"),
    ),
    (
        "engine-core/src/credits/mod.rs",
        include_str!("../credits/mod.rs"),
    ),
    (
        "engine-core/src/eval/mod.rs",
        include_str!("../eval/mod.rs"),
    ),
    (
        "engine-core/src/lessontest/mod.rs",
        include_str!("../lessontest/mod.rs"),
    ),
    (
        "engine-core/src/lessontest/retry.rs",
        include_str!("../lessontest/retry.rs"),
    ),
    (
        "engine-core/src/narration/mod.rs",
        include_str!("../narration/mod.rs"),
    ),
    (
        "engine-core/src/notes/mod.rs",
        include_str!("../notes/mod.rs"),
    ),
    (
        "engine-core/src/sharing/mod.rs",
        include_str!("../sharing/mod.rs"),
    ),
    (
        "engine-core/src/sharing/missing.rs",
        include_str!("../sharing/missing.rs"),
    ),
    (
        "engine-core/src/steering/mod.rs",
        include_str!("../steering/mod.rs"),
    ),
    (
        "engine-core/src/steering/replan.rs",
        include_str!("../steering/replan.rs"),
    ),
    (
        "engine-providers/src/lib.rs",
        include_str!("../../../engine-providers/src/lib.rs"),
    ),
    (
        "engine-providers/src/elevenlabs.rs",
        include_str!("../../../engine-providers/src/elevenlabs.rs"),
    ),
];

/// Source files that never shape a model call (backups, pricing tables, this gate itself).
pub const EXCLUDED_SOURCES: &[&str] = &[
    "engine-core/src/backup/mod.rs",
    "engine-core/src/pricing/mod.rs",
    "engine-core/src/promotion/mod.rs",
    // Redis cache and breaker for router decisions: they bound spend, never the prompt text.
    "engine-providers/src/state.rs",
    // engine-eval: command line, report writing and analytics rows decide nothing about whether a
    // case passed.
    "engine-eval/src/lib.rs",
    "engine-eval/src/main.rs",
    "engine-eval/src/report.rs",
    "engine-eval/src/analytics.rs",
    "engine-eval/src/compare.rs",
];

/// SHA-256 over the evaluated inputs, each framed by its path and length.
pub fn fingerprint() -> String {
    let mut ctx = ring::digest::Context::new(&ring::digest::SHA256);
    for (path, body) in SOURCES {
        ctx.update(path.as_bytes());
        ctx.update(&[0]);
        ctx.update(&(body.len() as u64).to_be_bytes());
        ctx.update(body.as_bytes());
    }
    hex(ctx.finish().as_ref())
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

/// Typed reason a live feature is refused. `code()` is the stable wire reason.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Denial {
    /// No live report is recorded (or it cannot be parsed).
    EvidenceMissing,
    /// The report does not satisfy the run-level promotion rule.
    EvidenceRejected(String),
    /// The report evaluated a different route, threshold set or prompt source.
    EvidenceStale,
    /// The run passed, but this feature's task has no passing live case.
    TaskNotPromoted,
}

impl Denial {
    pub fn code(&self) -> &'static str {
        match self {
            Denial::EvidenceMissing => "promotion_evidence_missing",
            Denial::EvidenceRejected(_) => "promotion_evidence_rejected",
            Denial::EvidenceStale => "promotion_evidence_stale",
            Denial::TaskNotPromoted => "promotion_task_not_passed",
        }
    }
    pub fn detail(&self) -> String {
        match self {
            Denial::EvidenceRejected(why) => why.clone(),
            other => other.code().to_string(),
        }
    }
}

#[derive(Debug, Deserialize)]
struct TaskReport {
    #[serde(default)]
    passed: bool,
    #[serde(default)]
    critical_failure: bool,
    #[serde(default)]
    total_cases: usize,
    #[serde(default)]
    score: f64,
    #[serde(default)]
    threshold: f64,
}

/// The subset of engine-eval's `EvalReport` the gate reads; unknown fields are ignored.
#[derive(Debug, Deserialize)]
struct Report {
    dataset: String,
    dataset_revision: String,
    dataset_source: String,
    #[serde(default)]
    dataset_verified: bool,
    #[serde(default)]
    dataset_sha256: String,
    provider: String,
    status: String,
    passed: bool,
    critical_failure: bool,
    #[serde(default)]
    budget_exceeded: bool,
    #[serde(default)]
    fingerprint: String,
    /// Cases in the pinned revision before any `--limit` sampling (0 in legacy reports).
    #[serde(default)]
    dataset_cases: usize,
    task_reports: BTreeMap<String, TaskReport>,
}

/// Evidence state for one build. `Gate::check(task)` is the only way to enable a live feature.
#[derive(Debug, Clone)]
pub struct Gate {
    run: Result<Summary, Denial>,
}

#[derive(Debug, Clone)]
pub struct Summary {
    pub dataset: String,
    pub dataset_revision: String,
    pub passed_tasks: Vec<String>,
}

impl Gate {
    /// Gate over the evidence embedded in this build.
    pub fn embedded() -> Self {
        Self::from_report(EVIDENCE, &fingerprint())
    }

    /// Gate over a report and the fingerprint of the running build.
    pub fn from_report(report: &str, current_fingerprint: &str) -> Self {
        Self {
            run: evaluate(report, current_fingerprint, PINS),
        }
    }

    /// A gate whose evidence was verified by the caller (test harnesses wiring stand-in
    /// providers). Production code uses `embedded()`; no runtime input reaches this.
    pub fn for_tasks(tasks: &[&str]) -> Self {
        Self {
            run: Ok(Summary {
                dataset: "verified-by-caller".into(),
                dataset_revision: String::new(),
                passed_tasks: tasks.iter().map(|t| t.to_string()).collect(),
            }),
        }
    }

    /// Ok when `task` may run on a live route.
    pub fn check(&self, task: &str) -> Result<(), Denial> {
        let summary = self.run.as_ref().map_err(Clone::clone)?;
        if summary.passed_tasks.iter().any(|t| t == task) {
            Ok(())
        } else {
            Err(Denial::TaskNotPromoted)
        }
    }

    pub fn summary(&self) -> Result<&Summary, &Denial> {
        self.run.as_ref()
    }

    /// Launch tasks this evidence leaves closed on a live engine, in `LAUNCH_TASKS` order.
    pub fn missing_launch_tasks(&self) -> Vec<&'static str> {
        LAUNCH_TASKS
            .into_iter()
            .filter(|t| self.check(t).is_err())
            .collect()
    }
}

impl Default for Gate {
    fn default() -> Self {
        Self::embedded()
    }
}

fn evaluate(raw: &str, current_fingerprint: &str, pins: &str) -> Result<Summary, Denial> {
    let report: Report = serde_json::from_str(raw).map_err(|_| Denial::EvidenceMissing)?;
    let reject = |why: &str| Err(Denial::EvidenceRejected(why.to_string()));
    if report.provider != "live" {
        return reject("provider is not live");
    }
    if report.status != "passed" || !report.passed {
        return reject("run did not pass");
    }
    if report.critical_failure {
        return reject("run has a critical failure");
    }
    if report.budget_exceeded {
        return reject("run exceeded its budget");
    }
    if !matches!(
        report.dataset_source.as_str(),
        "hf" | "mirror" | "cache" | "repo"
    ) {
        return reject("dataset source is not a pinned dataset");
    }
    if !report.dataset_verified {
        return reject("dataset bytes were not verified against the pin");
    }
    let is_hex = |s: &str, n: usize| s.len() == n && s.bytes().all(|b| b.is_ascii_hexdigit());
    if !is_hex(&report.dataset_revision, 40) {
        return reject("dataset revision is not a 40-character commit");
    }
    let pins: HashMap<String, HashMap<String, String>> =
        toml::from_str(pins).map_err(|_| Denial::EvidenceRejected("pins unreadable".into()))?;
    let pinned = pins
        .get(&report.dataset)
        .and_then(|revs| revs.get(&report.dataset_revision));
    match pinned {
        Some(digest) if digest.eq_ignore_ascii_case(&report.dataset_sha256) => {}
        Some(_) => return reject("dataset digest differs from the repository pin"),
        None => return reject("dataset revision is not pinned in the repository"),
    }
    // A sample can skip every must-block row of a screen; only the whole pinned set promotes.
    let evaluated: usize = report.task_reports.values().map(|t| t.total_cases).sum();
    if report.dataset_cases == 0 || evaluated < report.dataset_cases {
        return reject("report is a sample, not every case of the pinned revision");
    }
    if report.fingerprint != current_fingerprint {
        return Err(Denial::EvidenceStale);
    }
    let ok = |t: &TaskReport| {
        t.passed && !t.critical_failure && t.total_cases >= 1 && t.score >= t.threshold
    };
    for task in CANONICAL_TASKS {
        if !report.task_reports.get(task).is_some_and(ok) {
            return reject(&format!("canonical task {task} did not pass"));
        }
    }
    Ok(Summary {
        dataset: report.dataset,
        dataset_revision: report.dataset_revision,
        passed_tasks: report
            .task_reports
            .iter()
            .filter(|(_, t)| ok(t))
            .map(|(name, _)| name.clone())
            .collect(),
    })
}

#[cfg(test)]
mod tests;
