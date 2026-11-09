use crate::tasks::canonical_task_name;
use serde::{Deserialize, Serialize};
use std::collections::{BTreeMap, HashMap};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Check {
    pub kind: String,
    #[serde(default)]
    pub value: Option<String>,
    #[serde(default)]
    pub critical: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Case {
    pub id: String,
    pub task: String,
    pub input: serde_json::Value,
    #[serde(default)]
    pub rubric: String,
    #[serde(default)]
    pub checks: Vec<Check>,
}

fn default_minimum_score() -> f64 {
    1.0
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Thresholds {
    #[serde(default = "default_minimum_score")]
    pub minimum_score: f64,
    #[serde(default)]
    pub tasks: HashMap<String, f64>,
}

impl Default for Thresholds {
    fn default() -> Self {
        Self {
            minimum_score: 1.0,
            tasks: HashMap::new(),
        }
    }
}

impl Thresholds {
    pub fn threshold_for(&self, task: &str) -> f64 {
        let canonical = canonical_task_name(task);
        if let Some(&score) = self.tasks.get(task) {
            return score;
        }
        if let Some(&score) = self.tasks.get(&canonical) {
            return score;
        }
        self.minimum_score
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CaseResult {
    pub id: String,
    pub task: String,
    pub passed: bool,
    pub critical_failed: bool,
    pub deterministic_passed: bool,
    pub judge_passed: Option<bool>,
    pub errors: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TaskReport {
    pub task: String,
    pub model: String,
    pub score: f64,
    pub threshold: f64,
    pub passed: bool,
    pub critical_failure: bool,
    pub total_cases: usize,
    pub passed_cases: usize,
    pub deterministic_score: f64,
    pub judge_score: Option<f64>,
}

fn default_dataset_source() -> String {
    "fixture".to_string()
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EvalReport {
    pub timestamp_millis: u128,
    pub dataset: String,
    pub dataset_revision: String,
    #[serde(default = "default_dataset_source")]
    pub dataset_source: String,
    pub prompt_revision: String,
    pub passed: bool,
    pub critical_failure: bool,
    pub task_reports: BTreeMap<String, TaskReport>,
    pub results: Vec<CaseResult>,
}

pub fn aggregate_task_reports(
    case_results: &[CaseResult],
    thresholds: &Thresholds,
    models: &HashMap<String, String>,
) -> (BTreeMap<String, TaskReport>, bool, bool) {
    let mut by_task: BTreeMap<String, Vec<&CaseResult>> = BTreeMap::new();
    for res in case_results {
        by_task.entry(res.task.clone()).or_default().push(res);
    }

    let mut task_reports = BTreeMap::new();
    let mut overall_passed = true;
    let mut global_critical_failure = false;

    for (task, cases) in by_task {
        let total = cases.len();
        let passed_count = cases.iter().filter(|c| c.passed).count();
        let deterministic_pass_count = cases.iter().filter(|c| c.deterministic_passed).count();
        let task_critical_fail = cases.iter().any(|c| c.critical_failed);

        let judge_evaluated_cases: Vec<&CaseResult> = cases
            .iter()
            .filter(|c| c.judge_passed.is_some())
            .copied()
            .collect();
        let judge_score = if !judge_evaluated_cases.is_empty() {
            let pass_j = judge_evaluated_cases
                .iter()
                .filter(|c| c.judge_passed == Some(true))
                .count();
            Some(pass_j as f64 / judge_evaluated_cases.len() as f64)
        } else {
            None
        };

        let score = if total > 0 {
            passed_count as f64 / total as f64
        } else {
            0.0
        };
        let deterministic_score = if total > 0 {
            deterministic_pass_count as f64 / total as f64
        } else {
            0.0
        };

        let threshold = thresholds.threshold_for(&task);
        // Task passes only if score >= threshold AND no critical failure occurred in this task
        let passed = score >= threshold && !task_critical_fail;

        if !passed {
            overall_passed = false;
        }
        if task_critical_fail {
            global_critical_failure = true;
            overall_passed = false;
        }

        let model = models
            .get(&task)
            .cloned()
            .unwrap_or_else(|| "default".to_string());

        task_reports.insert(
            task.clone(),
            TaskReport {
                task,
                model,
                score,
                threshold,
                passed,
                critical_failure: task_critical_fail,
                total_cases: total,
                passed_cases: passed_count,
                deterministic_score,
                judge_score,
            },
        );
    }

    (task_reports, overall_passed, global_critical_failure)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn threshold_boundaries_evaluate_correctly() {
        let mut thresholds = Thresholds {
            minimum_score: 0.8,
            ..Default::default()
        };
        thresholds.tasks.insert("course_preview".into(), 0.9);
        thresholds.tasks.insert("preview".into(), 0.9);
        thresholds.tasks.insert("lesson_test".into(), 1.0);

        assert_eq!(thresholds.threshold_for("course_preview"), 0.9);
        assert_eq!(thresholds.threshold_for("preview"), 0.9);
        assert_eq!(thresholds.threshold_for("lesson_test"), 1.0);
        // fallback to minimum_score for unset tasks
        assert_eq!(thresholds.threshold_for("unknown_task"), 0.8);
    }

    #[test]
    fn critical_failure_immediately_fails_task_and_run_even_with_high_score() {
        let thresholds = Thresholds {
            minimum_score: 0.5,
            tasks: HashMap::new(),
        };

        // 3 cases: 2 pass, 1 fails a critical check
        let case_results = vec![
            CaseResult {
                id: "1".into(),
                task: "course_preview".into(),
                passed: true,
                critical_failed: false,
                deterministic_passed: true,
                judge_passed: None,
                errors: vec![],
            },
            CaseResult {
                id: "2".into(),
                task: "course_preview".into(),
                passed: true,
                critical_failed: false,
                deterministic_passed: true,
                judge_passed: None,
                errors: vec![],
            },
            CaseResult {
                id: "3".into(),
                task: "course_preview".into(),
                passed: false,
                critical_failed: true,
                deterministic_passed: false,
                judge_passed: None,
                errors: vec!["critical check failed".into()],
            },
        ];

        let models = HashMap::new();
        let (task_reports, overall_passed, critical_failure) =
            aggregate_task_reports(&case_results, &thresholds, &models);

        let report = &task_reports["course_preview"];
        // Score is 2/3 = 0.667 >= 0.5 threshold, BUT critical check failed!
        assert!(report.score >= report.threshold);
        assert!(!report.passed, "task must fail when critical check fails");
        assert!(report.critical_failure);
        assert!(!overall_passed, "eval run must fail on critical failure");
        assert!(critical_failure);
    }
}
