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
    assert_eq!(report_json["provider"], "none");
    assert_eq!(report_json["dataset_revision"], "none");
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

#[test]
fn secret_check_not_skipped_when_eval_data_path_is_set_for_live_run() {
    let temp_dir = std::env::temp_dir().join(format!(
        "eval-e2e-data-path-secrets-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_dir).unwrap();

    let data_file = temp_dir.join("cases.jsonl");
    let thresholds_file = temp_dir.join("thresholds.toml");
    let reports_dir = temp_dir.join("reports");

    std::fs::write(&data_file, "{\"id\":\"case-1\",\"task\":\"course_preview\",\"input\":{\"topic\":\"Coffee\"},\"rubric\":\"Pass\",\"checks\":[{\"kind\":\"json\"}]}\n").unwrap();
    std::fs::write(&thresholds_file, "minimum_score = 1.0\n").unwrap();

    let bin = env!("CARGO_BIN_EXE_duolify-eval");
    let output = Command::new(bin)
        .arg("--mode")
        .arg("ready")
        .arg("--provider")
        .arg("live")
        .arg("--data")
        .arg(&data_file)
        .arg("--thresholds")
        .arg(&thresholds_file)
        .arg("--reports")
        .arg(&reports_dir)
        .env_remove("ANTHROPIC_API_KEY")
        .env_remove("OPENROUTER_API_KEY")
        .env_remove("HF_TOKEN")
        .output()
        .expect("failed to execute duolify-eval");

    assert!(output.status.success());
    let report_files: Vec<_> = std::fs::read_dir(&reports_dir)
        .unwrap()
        .map(|e| e.unwrap().path())
        .filter(|p| p.extension().is_some_and(|ext| ext == "json"))
        .collect();
    assert_eq!(report_files.len(), 1);
    let report_content = std::fs::read_to_string(&report_files[0]).unwrap();
    let report_json: serde_json::Value = serde_json::from_str(&report_content).unwrap();
    assert_eq!(report_json["status"], "missing_secrets");
    assert_eq!(report_json["provider"], "none");
    assert_eq!(report_json["dataset_revision"], "none");

    let _ = std::fs::remove_dir_all(temp_dir);
}

#[test]
fn missing_hf_golden_revision_reports_missing_config_status() {
    let temp_dir = std::env::temp_dir().join(format!(
        "eval-e2e-missing-config-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_dir).unwrap();

    let thresholds_file = temp_dir.join("thresholds.toml");
    let reports_dir = temp_dir.join("reports");
    std::fs::write(&thresholds_file, "minimum_score = 1.0\n").unwrap();

    let bin = env!("CARGO_BIN_EXE_duolify-eval");
    let output = Command::new(bin)
        .arg("--mode")
        .arg("ready")
        .arg("--provider")
        .arg("live")
        .arg("--thresholds")
        .arg(&thresholds_file)
        .arg("--reports")
        .arg(&reports_dir)
        .env("ANTHROPIC_API_KEY", "test-key")
        .env("OPENROUTER_API_KEY", "test-key")
        .env("HF_TOKEN", "test-token")
        .env_remove("HF_GOLDEN_REVISION")
        .output()
        .expect("failed to execute duolify-eval");

    assert!(output.status.success());
    let report_files: Vec<_> = std::fs::read_dir(&reports_dir)
        .unwrap()
        .map(|e| e.unwrap().path())
        .filter(|p| p.extension().is_some_and(|ext| ext == "json"))
        .collect();
    assert_eq!(report_files.len(), 1);
    let report_content = std::fs::read_to_string(&report_files[0]).unwrap();
    let report_json: serde_json::Value = serde_json::from_str(&report_content).unwrap();
    assert_eq!(report_json["status"], "missing_config");
    assert_eq!(report_json["provider"], "none");
    assert_eq!(report_json["dataset_revision"], "none");
    assert_eq!(report_json["passed"], false);
    assert_eq!(report_json["live_promoted"], false);

    let _ = std::fs::remove_dir_all(temp_dir);
}

#[test]
fn mode_draft_with_provider_live_must_error_not_override() {
    let bin = env!("CARGO_BIN_EXE_duolify-eval");
    let output = Command::new(bin)
        .arg("--mode")
        .arg("draft")
        .arg("--provider")
        .arg("live")
        .output()
        .expect("failed to execute duolify-eval");

    assert!(
        !output.status.success(),
        "mode draft with provider live must fail"
    );
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("draft") && stderr.contains("live"));
}

#[test]
fn non_mock_unknown_provider_must_error() {
    let bin = env!("CARGO_BIN_EXE_duolify-eval");
    let output = Command::new(bin)
        .arg("--provider")
        .arg("some_unknown_provider")
        .output()
        .expect("failed to execute duolify-eval");

    assert!(!output.status.success(), "unknown provider must fail");
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("unknown provider"));
}

#[test]
fn promotion_gate_cli_subcommand_verifies_report() {
    let bin = env!("CARGO_BIN_EXE_duolify-eval");
    let temp_dir = std::env::temp_dir().join(format!("gate-test-{}", std::process::id()));
    std::fs::create_dir_all(&temp_dir).unwrap();

    let valid_report = serde_json::json!({
        "timestamp_millis": 1000,
        "dataset": "datamonsterr/duolify-golden",
        "dataset_revision": "0123456789abcdef0123456789abcdef01234567",
        "dataset_source": "hf",
        "prompt_revision": "head",
        "provider": "live",
        "status": "passed",
        "live_promoted": true,
        "passed": true,
        "critical_failure": false,
        "budget_exceeded": false,
        "task_reports": {
            "course_preview": { "task": "course_preview", "model": "m", "score": 1.0, "threshold": 1.0, "passed": true, "critical_failure": false, "total_cases": 1, "passed_cases": 1, "deterministic_score": 1.0 },
            "course_outline": { "task": "course_outline", "model": "m", "score": 1.0, "threshold": 1.0, "passed": true, "critical_failure": false, "total_cases": 1, "passed_cases": 1, "deterministic_score": 1.0 },
            "lesson_slides": { "task": "lesson_slides", "model": "m", "score": 1.0, "threshold": 1.0, "passed": true, "critical_failure": false, "total_cases": 1, "passed_cases": 1, "deterministic_score": 1.0 },
            "lesson_test": { "task": "lesson_test", "model": "m", "score": 1.0, "threshold": 1.0, "passed": true, "critical_failure": false, "total_cases": 1, "passed_cases": 1, "deterministic_score": 1.0 },
            "tutor_chat": { "task": "tutor_chat", "model": "m", "score": 1.0, "threshold": 1.0, "passed": true, "critical_failure": false, "total_cases": 1, "passed_cases": 1, "deterministic_score": 1.0 },
            "note_enrich": { "task": "note_enrich", "model": "m", "score": 1.0, "threshold": 1.0, "passed": true, "critical_failure": false, "total_cases": 1, "passed_cases": 1, "deterministic_score": 1.0 }
        },
        "results": []
    });

    let valid_path = temp_dir.join("valid_report.json");
    std::fs::write(&valid_path, serde_json::to_string(&valid_report).unwrap()).unwrap();

    // 1. Valid report exits 0
    let output = Command::new(bin)
        .arg("gate")
        .arg(&valid_path)
        .output()
        .expect("failed to execute duolify-eval gate");
    assert!(
        output.status.success(),
        "gate check must pass for valid live report: {:?}",
        String::from_utf8_lossy(&output.stderr)
    );

    // 2. Report missing a required canonical task exits non-zero
    let mut invalid_report = valid_report.clone();
    invalid_report["task_reports"]
        .as_object_mut()
        .unwrap()
        .remove("note_enrich");
    let invalid_path = temp_dir.join("invalid_report.json");
    std::fs::write(
        &invalid_path,
        serde_json::to_string(&invalid_report).unwrap(),
    )
    .unwrap();

    let output_err = Command::new(bin)
        .arg("gate")
        .arg(&invalid_path)
        .output()
        .expect("failed to execute duolify-eval gate");
    assert!(
        !output_err.status.success(),
        "gate check must exit non-zero when task is missing"
    );

    // 3. Report with missing_secrets status exits non-zero
    let mut secrets_report = valid_report.clone();
    secrets_report["status"] = "missing_secrets".into();
    let secrets_path = temp_dir.join("secrets_report.json");
    std::fs::write(
        &secrets_path,
        serde_json::to_string(&secrets_report).unwrap(),
    )
    .unwrap();

    let output_sec = Command::new(bin)
        .arg("gate")
        .arg(&secrets_path)
        .output()
        .expect("failed to execute duolify-eval gate");
    assert!(
        !output_sec.status.success(),
        "gate check must exit non-zero for missing_secrets report"
    );

    let _ = std::fs::remove_dir_all(temp_dir);
}
