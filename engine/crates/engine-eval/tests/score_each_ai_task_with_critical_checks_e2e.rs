mod common;

use std::process::Command;

#[test]
fn failing_critical_check_causes_nonzero_exit_even_with_passing_tasks() {
    let temp_dir = std::env::temp_dir().join(format!(
        "eval-e2e-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_dir).unwrap();

    let data_file = temp_dir.join("cases.jsonl");
    let thresholds_file = temp_dir.join("thresholds.toml");
    let reports_dir = temp_dir.join("reports");

    // Case 1: passing task (course_preview)
    // Case 2: failing critical check on another task (lesson_test)
    let cases = r#"{"id":"preview-pass","task":"course_preview","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
{"id":"test-critical-fail","task":"lesson_test","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"field","value":"non_existent_field_12345","critical":true}]}
"#;
    std::fs::write(&data_file, cases).unwrap();

    // Even if threshold is low (0.0 or 0.5), critical check failure must fail the run immediately
    let thresholds = r#"minimum_score = 0.5
[tasks]
course_preview = 0.5
lesson_test = 0.0
"#;
    std::fs::write(&thresholds_file, thresholds).unwrap();

    let bin = common::eval_bin();
    let output = Command::new(bin)
        .arg("--offline")
        .arg("--provider")
        .arg("mock")
        .arg("--data")
        .arg(&data_file)
        .arg("--thresholds")
        .arg(&thresholds_file)
        .arg("--reports")
        .arg(&reports_dir)
        .output()
        .expect("failed to execute duolify-eval");

    assert!(
        !output.status.success(),
        "expected duolify-eval to fail due to critical check failure, but exited with success. stdout: {}, stderr: {}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );

    let _ = std::fs::remove_dir_all(temp_dir);
}

#[test]
fn good_aggregate_does_not_hide_failing_task() {
    let temp_dir = std::env::temp_dir().join(format!(
        "eval-e2e-agg-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_dir).unwrap();

    let data_file = temp_dir.join("cases.jsonl");
    let thresholds_file = temp_dir.join("thresholds.toml");
    let reports_dir = temp_dir.join("reports");

    // 9 passing course_preview cases, 1 failing lesson_test case
    let mut cases = String::new();
    for i in 1..=9 {
        cases.push_str(&format!(r#"{{"id":"preview-{i}","task":"course_preview","input":{{"topic":"Coffee"}},"rubric":"Pass","checks":[{{"kind":"json"}}]}}"#));
        cases.push('\n');
    }
    cases.push_str(r#"{"id":"test-fail","task":"lesson_test","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"field","value":"non_existent_key"}]}"#);
    cases.push('\n');
    std::fs::write(&data_file, cases).unwrap();

    let thresholds = r#"minimum_score = 0.8
[tasks]
course_preview = 0.8
lesson_test = 1.0
"#;
    std::fs::write(&thresholds_file, thresholds).unwrap();

    let bin = common::eval_bin();
    let output = Command::new(bin)
        .arg("--offline")
        .arg("--provider")
        .arg("mock")
        .arg("--data")
        .arg(&data_file)
        .arg("--thresholds")
        .arg(&thresholds_file)
        .arg("--reports")
        .arg(&reports_dir)
        .output()
        .expect("failed to execute duolify-eval");

    // Aggregate is 9/10 = 90% >= 80%, but lesson_test is 0.0 < 1.0!
    assert!(
        !output.status.success(),
        "expected failure because lesson_test is below its threshold, but exited with success. stdout: {}, stderr: {}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );

    let _ = std::fs::remove_dir_all(temp_dir);
}
