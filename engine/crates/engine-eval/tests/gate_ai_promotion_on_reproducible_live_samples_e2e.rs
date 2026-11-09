use std::process::Command;

#[test]
fn draft_mock_pipeline_runs_free_and_offline() {
    let temp_dir = std::env::temp_dir().join(format!(
        "eval-e2e-draft-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_dir).unwrap();

    let data_file = temp_dir.join("cases.jsonl");
    let thresholds_file = temp_dir.join("thresholds.toml");
    let reports_dir = temp_dir.join("reports");

    let cases = r#"{"id":"preview-1","task":"course_preview","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
{"id":"outline-1","task":"course_outline","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
"#;
    std::fs::write(&data_file, cases).unwrap();

    let thresholds = r#"minimum_score = 1.0
[tasks]
course_preview = 1.0
course_outline = 1.0
"#;
    std::fs::write(&thresholds_file, thresholds).unwrap();

    let bin = env!("CARGO_BIN_EXE_duolify-eval");
    let output = Command::new(bin)
        .arg("--mode")
        .arg("draft")
        .arg("--data")
        .arg(&data_file)
        .arg("--thresholds")
        .arg(&thresholds_file)
        .arg("--reports")
        .arg(&reports_dir)
        .output()
        .expect("failed to execute duolify-eval");

    assert!(
        output.status.success(),
        "draft mock pipeline must succeed, stdout: {}, stderr: {}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );

    // Verify report artifact was created in reports_dir
    let report_files: Vec<_> = std::fs::read_dir(&reports_dir)
        .unwrap()
        .map(|e| e.unwrap().path())
        .filter(|p| p.extension().is_some_and(|ext| ext == "json"))
        .collect();
    assert_eq!(
        report_files.len(),
        1,
        "must produce exactly one JSON report artifact"
    );

    let report_content = std::fs::read_to_string(&report_files[0]).unwrap();
    let report_json: serde_json::Value = serde_json::from_str(&report_content).unwrap();
    assert_eq!(report_json["passed"], true);
    assert_eq!(report_json["provider"], "mock");
    assert_eq!(report_json["total_cost_usd"], 0.0);

    let _ = std::fs::remove_dir_all(temp_dir);
}

#[test]
fn missing_secret_ready_pipeline_reports_explicit_status_keeps_ci_ok_and_disables_promotion() {
    let temp_dir = std::env::temp_dir().join(format!(
        "eval-e2e-missing-secrets-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_dir).unwrap();

    let reports_dir = temp_dir.join("reports");
    let thresholds_file = temp_dir.join("thresholds.toml");
    std::fs::write(&thresholds_file, "minimum_score = 1.0\n").unwrap();

    let bin = env!("CARGO_BIN_EXE_duolify-eval");
    // Run in ready pipeline mode with missing / empty credentials
    let output = Command::new(bin)
        .arg("--mode")
        .arg("ready")
        .arg("--thresholds")
        .arg(&thresholds_file)
        .arg("--reports")
        .arg(&reports_dir)
        .env_remove("ANTHROPIC_API_KEY")
        .env_remove("OPENROUTER_API_KEY")
        .env_remove("HF_TOKEN")
        .env(
            "HF_GOLDEN_REVISION",
            "0123456789abcdef0123456789abcdef01234567",
        )
        .output()
        .expect("failed to execute duolify-eval");

    let stdout = String::from_utf8_lossy(&output.stdout);
    let stderr = String::from_utf8_lossy(&output.stderr);

    // Must exit with code 0 to keep ci-ok aggregate passing in CI
    assert!(
        output.status.success(),
        "ready mode with missing secrets must exit 0 to preserve ci-ok aggregate, stdout: {stdout}, stderr: {stderr}"
    );

    // Must explicitly log missing-secret status and notice
    assert!(
        stdout.contains("missing-secret")
            || stdout.contains("missing credentials")
            || stdout.contains("missing_secrets")
            || stderr.contains("missing-secret")
            || stderr.contains("missing credentials")
            || stderr.contains("missing_secrets"),
        "must log explicit missing-secret status. stdout: {stdout}, stderr: {stderr}"
    );

    // Verify report artifact exists with 90-day retention contract
    let report_files: Vec<_> = std::fs::read_dir(&reports_dir)
        .unwrap()
        .map(|e| e.unwrap().path())
        .filter(|p| p.extension().is_some_and(|ext| ext == "json"))
        .collect();
    assert_eq!(
        report_files.len(),
        1,
        "must produce JSON report artifact for missing secrets"
    );

    let report_content = std::fs::read_to_string(&report_files[0]).unwrap();
    let report_json: serde_json::Value = serde_json::from_str(&report_content).unwrap();
    assert_eq!(report_json["status"], "missing_secrets");
    assert_eq!(
        report_json["passed"], false,
        "must NOT fabricate a live pass"
    );
    assert_eq!(
        report_json["live_promoted"], false,
        "feature activation must remain disabled"
    );

    let _ = std::fs::remove_dir_all(temp_dir);
}

#[test]
fn budget_exceeded_live_fixture_pipeline_stops_at_budget_and_fails() {
    let temp_dir = std::env::temp_dir().join(format!(
        "eval-e2e-budget-exceeded-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_dir).unwrap();

    let data_file = temp_dir.join("cases.jsonl");
    let thresholds_file = temp_dir.join("thresholds.toml");
    let reports_dir = temp_dir.join("reports");

    // 5 cases, each costing some budget
    let mut cases = String::new();
    for i in 1..=5 {
        cases.push_str(&format!(
            r#"{{"id":"case-{i}","task":"course_preview","input":{{"topic":"Coffee {i}"}},"rubric":"Pass","checks":[{{"kind":"json"}}]}}"#
        ));
        cases.push('\n');
    }
    std::fs::write(&data_file, cases).unwrap();
    std::fs::write(&thresholds_file, "minimum_score = 0.5\n").unwrap();

    let bin = env!("CARGO_BIN_EXE_duolify-eval");
    // Run with very low eval budget: $0.000001 (1000 nanos), so first or second case exceeds it
    let output = Command::new(bin)
        .arg("--provider")
        .arg("mock")
        .arg("--data")
        .arg(&data_file)
        .arg("--thresholds")
        .arg(&thresholds_file)
        .arg("--reports")
        .arg(&reports_dir)
        .arg("--budget")
        .arg("0.000001")
        .output()
        .expect("failed to execute duolify-eval");

    let stdout = String::from_utf8_lossy(&output.stdout);
    let stderr = String::from_utf8_lossy(&output.stderr);

    assert!(
        !output.status.success(),
        "budget exceeded eval must fail, stdout: {stdout}, stderr: {stderr}"
    );

    assert!(
        stdout.contains("budget") || stderr.contains("budget"),
        "stdout or stderr must indicate budget exceeded. stdout: {stdout}, stderr: {stderr}"
    );

    // Verify report artifact exists and marks budget_exceeded
    let report_files: Vec<_> = std::fs::read_dir(&reports_dir)
        .unwrap()
        .map(|e| e.unwrap().path())
        .filter(|p| p.extension().is_some_and(|ext| ext == "json"))
        .collect();
    assert_eq!(report_files.len(), 1, "must produce JSON report artifact");

    let report_content = std::fs::read_to_string(&report_files[0]).unwrap();
    let report_json: serde_json::Value = serde_json::from_str(&report_content).unwrap();
    assert_eq!(report_json["budget_exceeded"], true);
    assert_eq!(report_json["passed"], false);

    let _ = std::fs::remove_dir_all(temp_dir);
}
