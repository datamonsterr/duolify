use engine_eval::rubric::{check_family, parse_verdict, static_checks};
use engine_eval::{CaseResult, Thresholds, aggregate_task_reports, generate_markdown_report};
use serde_json::json;
use std::collections::HashMap;

#[test]
fn judge_requires_evidence_and_a_distinct_family() {
    assert!(parse_verdict(r#"{"verdict":"PASS"}"#, "example").is_err());
    assert!(parse_verdict(r#"{"verdict":"PASS","evidence":"{"}"#, "{example}").is_err());
    assert!(parse_verdict(r#"{"verdict":"PASS","evidence":"example"}"#, "an example").unwrap());
    assert!(check_family("anthropic/claude-haiku-5.5", "anthropic/claude-sonnet-5.5").is_err());
    assert!(check_family("anthropic/claude-haiku-5.5", "tencent/hy4-flash").is_ok());
}

#[test]
fn criterion_rates_are_reported_and_gate_the_task() {
    let mut thresholds = Thresholds::default();
    thresholds.quality.insert("R5".into(), 0.75);
    let results = vec![
        CaseResult {
            task: "lesson_slides".into(),
            passed: true,
            deterministic_passed: true,
            quality: [("R5".into(), true)].into(),
            ..Default::default()
        },
        CaseResult {
            task: "lesson_slides".into(),
            passed: true,
            deterministic_passed: true,
            quality: [("R5".into(), false)].into(),
            ..Default::default()
        },
    ];
    let (reports, passed, _) = aggregate_task_reports(&results, &thresholds, &HashMap::new());
    assert!(!passed);
    assert_eq!(reports["lesson_slides"].quality["R5"].pass_rate, 0.5);
    let report = engine_eval::EvalReport {
        task_reports: reports,
        ..Default::default()
    };
    assert!(
        generate_markdown_report(&report)
            .contains("| lesson_slides | R5 | 0.50 | 0.75 | 2 | FAIL |")
    );
    assert_eq!(
        serde_json::to_value(report).unwrap()["task_reports"]["lesson_slides"]["quality"]["R5"]["pass_rate"],
        0.5
    );
}

#[tokio::test]
async fn quality_judge_fixture_is_used_per_criterion_and_rejects_missing_evidence() {
    let case = engine_eval::Case {
        id: "lesson".into(),
        task: "lesson_slides".into(),
        input: json!({"goal":"Identify a sample", "locale":"en"}),
        rubric: String::new(),
        checks: vec![],
    };
    let mut thresholds = Thresholds::default();
    thresholds.quality.insert("R5".into(), 0.5);
    let runner = engine_eval::Runner::mock_with_judge_judgments(vec![(
        "lesson:R5".into(),
        r#"{"verdict":"FAIL","evidence":"Sample"}"#.into(),
    )]);
    let report = runner
        .run_cases(vec![case.clone()], &thresholds)
        .await
        .unwrap();
    assert!(!report.results[0].quality["R5"]);
    assert!(!report.passed);
    let runner = engine_eval::Runner::mock_with_judge_judgments(vec![(
        "lesson:R5".into(),
        r#"{"verdict":"PASS"}"#.into(),
    )]);
    assert!(runner.run_cases(vec![case], &thresholds).await.is_err());
}

#[test]
fn deterministic_checks_detect_redundancy_and_length() {
    let input = json!({"locale":"en", "goal":"Identify values", "lesson_length":"short"});
    let lesson = json!({"slides":[{"html":"<p>hello world</p>","narration":"hello world","duration_ms":1000}]});
    let checks = static_checks(&input, &lesson);
    assert_eq!(checks.get("R9"), Some(&false));
    assert_eq!(checks.get("R10"), Some(&false));
}
