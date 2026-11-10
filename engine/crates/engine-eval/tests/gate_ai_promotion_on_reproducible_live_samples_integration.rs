use engine_eval::{
    Case, Check, EvalReport, Runner, TaskReport, Thresholds, generate_json_report,
    generate_markdown_report, is_promotable, select_cases,
};
use std::collections::{BTreeMap, HashMap};

fn make_stratified_test_cases() -> Vec<Case> {
    let mut cases = Vec::new();
    // 3 tasks, 4 cases each
    for task in ["course_preview", "course_outline", "lesson_slides"] {
        for i in 1..=4 {
            cases.push(Case {
                id: format!("{task}-{i:02}"),
                task: task.to_string(),
                input: serde_json::json!({"topic": "Astronomy"}),
                rubric: "Pass".to_string(),
                checks: vec![Check {
                    kind: "json".to_string(),
                    value: None,
                    critical: false,
                }],
            });
        }
    }
    cases
}

#[test]
fn ci_fixtures_and_task_stratified_bounded_sample_selection() {
    let cases = make_stratified_test_cases();
    assert_eq!(cases.len(), 12);

    // Limit = 6: should select 2 cases from each of the 3 tasks (stratified round-robin)
    let (selected_6, total) = select_cases(cases.clone(), Some(6));
    assert_eq!(total, 12);
    assert_eq!(selected_6.len(), 6);

    let mut task_counts: HashMap<String, usize> = HashMap::new();
    for c in &selected_6 {
        *task_counts.entry(c.task.clone()).or_default() += 1;
    }
    assert_eq!(task_counts.get("course_preview"), Some(&2));
    assert_eq!(task_counts.get("course_outline"), Some(&2));
    assert_eq!(task_counts.get("lesson_slides"), Some(&2));

    // Limit = 4: round-robin should select at least 1 from each task (e.g. 2, 1, 1)
    let (selected_4, _) = select_cases(cases.clone(), Some(4));
    assert_eq!(selected_4.len(), 4);
    let mut task_counts_4: HashMap<String, usize> = HashMap::new();
    for c in &selected_4 {
        *task_counts_4.entry(c.task.clone()).or_default() += 1;
    }
    assert!(task_counts_4.values().all(|&cnt| cnt >= 1));

    // Limit larger than total: selects all
    let (selected_all, _) = select_cases(cases.clone(), Some(20));
    assert_eq!(selected_all.len(), 12);

    // Limit None or 0: selects all
    let (selected_none, _) = select_cases(cases.clone(), None);
    assert_eq!(selected_none.len(), 12);
}

#[tokio::test]
async fn provider_budget_accounting_stops_at_eval_budget_and_records_usage() {
    let cases = make_stratified_test_cases();
    let thresholds = Thresholds::default();

    // Set a tiny budget: 0.000001 USD
    let mut runner = Runner::new("mock", false);
    runner.budget_usd = Some(0.000001);

    let report = runner.run_cases(cases, &thresholds).await.unwrap();

    // Must halt when budget is exceeded
    assert!(report.budget_exceeded, "must set budget_exceeded flag");
    assert!(!report.passed, "run must not pass when budget is exceeded");
    assert!(report.total_cost_usd > 0.0, "total cost must be accounted");
    assert_eq!(report.budget_usd, Some(0.000001));

    // Execution must have stopped before evaluating all 12 cases
    assert!(
        report.results.len() < 12,
        "must stop early instead of running all 12 cases; ran {}",
        report.results.len()
    );
}

#[test]
fn artifact_report_contracts_record_case_revision_provider_usage_budget_thresholds() {
    let mut task_reports = BTreeMap::new();
    task_reports.insert(
        "course_preview".to_string(),
        TaskReport {
            task: "course_preview".to_string(),
            model: "claude-sonnet-5-5".to_string(),
            score: 1.0,
            threshold: 1.0,
            passed: true,
            critical_failure: false,
            total_cases: 2,
            passed_cases: 2,
            deterministic_score: 1.0,
            judge_score: None,
        },
    );

    let report = EvalReport {
        timestamp_millis: 1700000000000,
        dataset: "datamonsterr/duolify-golden".to_string(),
        dataset_revision: "0123456789abcdef0123456789abcdef01234567".to_string(),
        dataset_source: "fixture".to_string(),
        prompt_revision: "main-head".to_string(),
        provider: "live".to_string(),
        budget_usd: Some(10.0),
        total_cost_usd: 0.045,
        budget_exceeded: false,
        status: "passed".to_string(),
        live_promoted: true,
        passed: true,
        critical_failure: false,
        task_reports,
        results: vec![],
    };

    // JSON contract
    let json_bytes = generate_json_report(&report).unwrap();
    let json_val: serde_json::Value = serde_json::from_slice(&json_bytes).unwrap();

    assert_eq!(
        json_val["dataset_revision"],
        "0123456789abcdef0123456789abcdef01234567"
    );
    assert_eq!(json_val["provider"], "live");
    assert_eq!(json_val["budget_usd"], 10.0);
    assert_eq!(json_val["total_cost_usd"], 0.045);
    assert_eq!(json_val["status"], "passed");
    assert_eq!(json_val["live_promoted"], true);

    // Markdown contract
    let md = generate_markdown_report(&report);
    assert!(md.contains("0123456789abcdef0123456789abcdef01234567"));
    assert!(md.contains("live"));
    assert!(md.contains("10.00"));
    assert!(md.contains("0.045"));
}

#[tokio::test]
async fn critical_safety_ledger_cost_violations_are_rejected() {
    let case = Case {
        id: "critical-violation-1".to_string(),
        task: "course_preview".to_string(),
        input: serde_json::json!({"topic": "Hacking injection <script>"}),
        rubric: "Must be safe".to_string(),
        checks: vec![Check {
            kind: "contains".to_string(),
            value: Some("impossible_token_string_safe".to_string()),
            critical: true, // critical check fails
        }],
    };

    let runner = Runner::new("mock", false);
    let thresholds = Thresholds {
        minimum_score: 0.0, // even with 0.0 minimum score
        tasks: HashMap::new(),
    };

    let report = runner.run_cases(vec![case], &thresholds).await.unwrap();

    assert!(report.critical_failure, "critical failure must be flagged");
    assert!(
        !report.passed,
        "overall run must fail on critical violation"
    );
    assert!(
        report.results[0].critical_failed,
        "case result must record critical failure"
    );
}

#[test]
fn feature_promotion_verification_disallows_missing_secrets_and_fixture_passes() {
    let mut task_reports = BTreeMap::new();
    task_reports.insert(
        "course_brief_chat".to_string(),
        TaskReport {
            task: "course_brief_chat".to_string(),
            model: "claude-haiku-5-5".to_string(),
            score: 1.0,
            threshold: 1.0,
            passed: true,
            critical_failure: false,
            total_cases: 5,
            passed_cases: 5,
            deterministic_score: 1.0,
            judge_score: None,
        },
    );

    // Case 1: Mock/fixture run -> cannot promote
    let mock_report = EvalReport {
        timestamp_millis: 1000,
        dataset: "datamonsterr/duolify-golden".to_string(),
        dataset_revision: "0123456789abcdef0123456789abcdef01234567".to_string(),
        dataset_source: "fixture".to_string(),
        prompt_revision: "head".to_string(),
        provider: "mock".to_string(),
        budget_usd: Some(5.0),
        total_cost_usd: 0.0,
        budget_exceeded: false,
        status: "passed".to_string(),
        live_promoted: false,
        passed: true,
        critical_failure: false,
        task_reports: task_reports.clone(),
        results: vec![],
    };
    assert!(
        !is_promotable(&mock_report, "course_brief_chat"),
        "mock runs cannot promote a live feature"
    );

    // Case 2: Missing secrets run -> cannot promote
    let missing_secrets_report = EvalReport {
        timestamp_millis: 1000,
        dataset: "datamonsterr/duolify-golden".to_string(),
        dataset_revision: "0123456789abcdef0123456789abcdef01234567".to_string(),
        dataset_source: "hf".to_string(),
        prompt_revision: "head".to_string(),
        provider: "live".to_string(),
        budget_usd: Some(5.0),
        total_cost_usd: 0.0,
        budget_exceeded: false,
        status: "missing_secrets".to_string(),
        live_promoted: false,
        passed: false,
        critical_failure: false,
        task_reports: task_reports.clone(),
        results: vec![],
    };
    assert!(
        !is_promotable(&missing_secrets_report, "course_brief_chat"),
        "missing secrets cannot promote a live feature"
    );

    // Case 3: Genuine passing live run on pinned revision -> CAN promote
    // Note: requires all 6 canonical tasks for full promotion gate
    for task in engine_eval::CANONICAL_TASKS {
        task_reports
            .entry(task.to_string())
            .or_insert_with(|| TaskReport {
                task: task.to_string(),
                model: "claude-haiku-5-5".to_string(),
                score: 1.0,
                threshold: 1.0,
                passed: true,
                critical_failure: false,
                total_cases: 5,
                passed_cases: 5,
                deterministic_score: 1.0,
                judge_score: None,
            });
    }

    let valid_live_report = EvalReport {
        timestamp_millis: 1000,
        dataset: "datamonsterr/duolify-golden".to_string(),
        dataset_revision: "0123456789abcdef0123456789abcdef01234567".to_string(),
        dataset_source: "hf".to_string(),
        prompt_revision: "head".to_string(),
        provider: "live".to_string(),
        budget_usd: Some(5.0),
        total_cost_usd: 0.02,
        budget_exceeded: false,
        status: "passed".to_string(),
        live_promoted: true,
        passed: true,
        critical_failure: false,
        task_reports,
        results: vec![],
    };
    assert!(
        is_promotable(&valid_live_report, "course_brief_chat"),
        "valid passing live run must be promotable"
    );
}

struct RecordedProvider {
    primary_completion: engine_core::Completion,
    judge_completion: engine_core::Completion,
}

#[async_trait::async_trait]
impl engine_core::LlmProvider for RecordedProvider {
    fn name(&self) -> &'static str {
        "recorded"
    }
    async fn complete(&self, model: &str, _input: &str) -> anyhow::Result<engine_core::Completion> {
        if model.contains("haiku") {
            Ok(self.judge_completion.clone())
        } else {
            Ok(self.primary_completion.clone())
        }
    }
    async fn stream(
        &self,
        _model: &str,
        _input: &str,
    ) -> anyhow::Result<engine_core::CompletionStream> {
        anyhow::bail!("not streaming in recorded provider test")
    }
}

#[tokio::test]
async fn test_recorded_provider_budget_sums_primary_and_judge_using_price_table() {
    use std::sync::Arc;
    let provider = Arc::new(RecordedProvider {
        primary_completion: engine_core::Completion {
            text: "{\"title\":\"Sample Title\",\"units\":[]}".into(),
            provider: "recorded".into(),
            model: "claude-sonnet-5-5".into(),
            input_tokens: 100,
            output_tokens: 50,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: false,
        },
        judge_completion: engine_core::Completion {
            text: "PASS".into(),
            provider: "recorded".into(),
            model: "claude-haiku-5-5".into(),
            input_tokens: 40,
            output_tokens: 10,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: false,
        },
    });

    let router = engine_core::ProviderRouter::new(provider.clone(), provider).unwrap();
    let runner = Runner::with_router(router, "recorded", true);

    let cases = vec![Case {
        id: "preview-01".into(),
        task: "course_preview".into(),
        input: serde_json::json!({
            "limits": {
                "input_tokens": 4000,
                "output_tokens": 2500,
                "timeout_ms": 3000,
                "max_attempts": 2,
                "max_cost_nanos": 100000000,
                "input_nanos": 2000,
                "output_nanos": 10000,
                "cache_write_nanos": 2500,
                "cache_read_nanos": 100,
                "deadline_unix_ms": 0,
            }
        }),
        rubric: "Valid outline and preview".into(),
        checks: vec![Check {
            kind: "json".into(),
            value: None,
            critical: false,
        }],
    }];

    let thresholds = Thresholds::default();
    let report = runner.run_cases(cases, &thresholds).await.unwrap();

    assert!(report.passed);
    assert_eq!(report.results.len(), 1);
    let result = &report.results[0];

    // Primary: 100 in * 2000 nanos + 50 out * 10000 nanos = 700_000 nanos ($0.0007)
    // Judge: 40 in * 1000 nanos + 10 out * 5000 nanos = 90_000 nanos ($0.00009)
    // Total: 790_000 nanos ($0.00079)
    let expected_cost = 0.000790;
    assert!(
        (result.cost_usd - expected_cost).abs() < 1e-6,
        "expected cost {:.6}, got {:.6}",
        expected_cost,
        result.cost_usd
    );
    assert_eq!(result.input_tokens, 140);
    assert_eq!(result.output_tokens, 60);
    assert!(
        (report.total_cost_usd - expected_cost).abs() < 1e-6,
        "expected total cost {:.6}, got {:.6}",
        expected_cost,
        report.total_cost_usd
    );
}

#[tokio::test]
async fn test_recorded_provider_fails_closed_on_primary_usage_unavailable() {
    use std::sync::Arc;
    let provider = Arc::new(RecordedProvider {
        primary_completion: engine_core::Completion {
            text: "{\"title\":\"Sample Title\",\"units\":[]}".into(),
            provider: "recorded".into(),
            model: "claude-sonnet-5-5".into(),
            input_tokens: 0,
            output_tokens: 0,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: true, // Primary usage unavailable!
        },
        judge_completion: engine_core::Completion {
            text: "PASS".into(),
            provider: "recorded".into(),
            model: "claude-haiku-5-5".into(),
            input_tokens: 40,
            output_tokens: 10,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: false,
        },
    });

    let router = engine_core::ProviderRouter::new(provider.clone(), provider).unwrap();
    let runner = Runner::with_router(router, "recorded", true);

    let cases = vec![Case {
        id: "preview-01".into(),
        task: "course_preview".into(),
        input: serde_json::json!({}),
        rubric: "Valid preview".into(),
        checks: vec![],
    }];

    let thresholds = Thresholds::default();
    let report = runner.run_cases(cases, &thresholds).await.unwrap();

    assert!(
        !report.passed,
        "must fail closed when primary usage is unavailable"
    );
    assert_eq!(report.status, "critical_failure");
    assert!(
        report.results[0]
            .errors
            .iter()
            .any(|e| e.contains("usage_unavailable")),
        "must report usage_unavailable error"
    );
}

#[tokio::test]
async fn test_recorded_provider_fails_closed_on_judge_usage_unavailable() {
    use std::sync::Arc;
    let provider = Arc::new(RecordedProvider {
        primary_completion: engine_core::Completion {
            text: "{\"title\":\"Sample Title\",\"units\":[]}".into(),
            provider: "recorded".into(),
            model: "claude-sonnet-5-5".into(),
            input_tokens: 100,
            output_tokens: 50,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: false,
        },
        judge_completion: engine_core::Completion {
            text: "PASS".into(),
            provider: "recorded".into(),
            model: "claude-haiku-5-5".into(),
            input_tokens: 0,
            output_tokens: 0,
            cache_write_tokens: 0,
            cache_read_tokens: 0,
            usage_unavailable: true, // Judge usage unavailable!
        },
    });

    let router = engine_core::ProviderRouter::new(provider.clone(), provider).unwrap();
    let runner = Runner::with_router(router, "recorded", true);

    let cases = vec![Case {
        id: "preview-01".into(),
        task: "course_preview".into(),
        input: serde_json::json!({}),
        rubric: "Valid preview".into(),
        checks: vec![],
    }];

    let thresholds = Thresholds::default();
    let report = runner.run_cases(cases, &thresholds).await.unwrap();

    assert!(
        !report.passed,
        "must fail closed when judge usage is unavailable"
    );
    assert_eq!(report.status, "critical_failure");
    assert!(
        report.results[0]
            .errors
            .iter()
            .any(|e| e.contains("usage_unavailable")),
        "must report judge usage_unavailable error"
    );
}

#[test]
fn test_promotion_gate_enforces_all_canonical_tasks_and_minimum_coverage() {
    use engine_eval::check_promotion_gate;

    let mut task_reports = BTreeMap::new();
    for task in engine_eval::CANONICAL_TASKS {
        task_reports.insert(
            task.to_string(),
            TaskReport {
                task: task.to_string(),
                model: "claude-haiku-5-5".to_string(),
                score: 1.0,
                threshold: 1.0,
                passed: true,
                critical_failure: false,
                total_cases: 2,
                passed_cases: 2,
                deterministic_score: 1.0,
                judge_score: None,
            },
        );
    }

    let valid_report = EvalReport {
        timestamp_millis: 1000,
        dataset: "datamonsterr/duolify-golden".to_string(),
        dataset_revision: "0123456789abcdef0123456789abcdef01234567".to_string(),
        dataset_source: "hf".to_string(),
        prompt_revision: "head".to_string(),
        provider: "live".to_string(),
        budget_usd: Some(5.0),
        total_cost_usd: 0.05,
        budget_exceeded: false,
        status: "passed".to_string(),
        live_promoted: true,
        passed: true,
        critical_failure: false,
        task_reports: task_reports.clone(),
        results: vec![],
    };

    // 1. Fully valid -> Ok
    assert!(check_promotion_gate(&valid_report).is_ok());

    // 2. Missing a canonical task -> Err
    let mut missing_task_report = valid_report.clone();
    missing_task_report.task_reports.remove("tutor_chat");
    let res = check_promotion_gate(&missing_task_report);
    assert!(res.is_err(), "missing canonical task must fail gate");
    assert!(
        res.unwrap_err()
            .to_string()
            .contains("missing required canonical task 'tutor_chat'")
    );

    // 3. Zero coverage on a canonical task -> Err
    let mut zero_cov_report = valid_report.clone();
    zero_cov_report
        .task_reports
        .get_mut("course_preview")
        .unwrap()
        .total_cases = 0;
    let res = check_promotion_gate(&zero_cov_report);
    assert!(res.is_err(), "zero coverage must fail gate");
    assert!(
        res.unwrap_err()
            .to_string()
            .contains("zero evaluated cases")
    );

    // 4. Missing secrets status -> Err
    let mut missing_sec_report = valid_report.clone();
    missing_sec_report.status = "missing_secrets".to_string();
    assert!(check_promotion_gate(&missing_sec_report).is_err());

    // 5. Budget exceeded -> Err
    let mut budget_exc_report = valid_report.clone();
    budget_exc_report.budget_exceeded = true;
    assert!(check_promotion_gate(&budget_exc_report).is_err());
}
