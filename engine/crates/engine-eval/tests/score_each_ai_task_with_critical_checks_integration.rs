use engine_eval::{
    Case, Check, Runner, TaskRegistry, Thresholds, canonical_task_name, model_route_for_task,
    parse_judge_verdict,
};
use wiremock::{
    Mock, MockServer, ResponseTemplate,
    matchers::{method, path},
};

#[test]
fn task_alias_routing_canonicalizes_and_maps_routes() {
    // Legacy aliases map to canonical task names
    assert_eq!(canonical_task_name("preview"), "course_preview");
    assert_eq!(canonical_task_name("course_preview"), "course_preview");
    assert_eq!(canonical_task_name("outline"), "course_outline");
    assert_eq!(canonical_task_name("course_outline"), "course_outline");
    assert_eq!(canonical_task_name("lesson"), "lesson_slides");
    assert_eq!(canonical_task_name("slides"), "lesson_slides");
    assert_eq!(canonical_task_name("lesson_slides"), "lesson_slides");
    assert_eq!(canonical_task_name("test"), "lesson_test");
    assert_eq!(canonical_task_name("lesson_test"), "lesson_test");
    assert_eq!(canonical_task_name("chat"), "tutor_chat");
    assert_eq!(canonical_task_name("tutor_chat"), "tutor_chat");
    assert_eq!(canonical_task_name("note"), "note_enrich");
    assert_eq!(canonical_task_name("note_enrich"), "note_enrich");

    // Canonical tasks map to models.toml route names
    assert_eq!(model_route_for_task("course_preview"), "preview");
    assert_eq!(model_route_for_task("course_outline"), "outline");
    assert_eq!(model_route_for_task("lesson_slides"), "lesson");
    assert_eq!(model_route_for_task("lesson_test"), "lesson");
    assert_eq!(model_route_for_task("tutor_chat"), "chat");
    assert_eq!(model_route_for_task("note_enrich"), "note");

    // Extension registration
    let mut registry = TaskRegistry::default();
    registry.register_extension("variants");
    registry.register_extension("narration");
    registry.register_extension("screening");
    registry.register_extension("steering");

    assert!(registry.is_valid_task("variants"));
    assert!(registry.is_valid_task("narration"));
    assert!(registry.is_valid_task("screening"));
    assert!(registry.is_valid_task("steering"));
    assert!(registry.is_valid_task("course_preview"));

    // Feature gate checks: missing canonical task cases fail
    let present_tasks = vec!["course_preview".to_string(), "course_outline".to_string()];
    let missing = registry.missing_required_tasks(&present_tasks);
    assert!(
        !missing.is_empty(),
        "feature gate must report missing canonical tasks"
    );
    assert!(missing.contains(&"lesson_slides".to_string()));
    assert!(missing.contains(&"lesson_test".to_string()));
    assert!(missing.contains(&"tutor_chat".to_string()));
    assert!(missing.contains(&"note_enrich".to_string()));
}

#[test]
fn judge_result_parsing_handles_verdicts_and_explanations() {
    assert!(parse_judge_verdict("PASS").unwrap());
    assert!(parse_judge_verdict("pass").unwrap());
    assert!(parse_judge_verdict("PASS\n").unwrap());
    assert!(parse_judge_verdict("The rubric is satisfied.\nPASS").unwrap());
    assert!(parse_judge_verdict(r#"{"verdict":"PASS"}"#).unwrap());
    assert!(parse_judge_verdict(r#"{"passed":true}"#).unwrap());

    assert!(!parse_judge_verdict("FAIL").unwrap());
    assert!(!parse_judge_verdict("fail").unwrap());
    assert!(!parse_judge_verdict("The response lacks required content.\nFAIL").unwrap());
    assert!(!parse_judge_verdict(r#"{"verdict":"FAIL"}"#).unwrap());
    assert!(!parse_judge_verdict(r#"{"passed":false}"#).unwrap());
}

#[tokio::test]
async fn judge_calibration_prevents_good_aggregate_from_hiding_failing_task() {
    // Mock runner with calibration: Task A passes judge, Task B fails judge
    let cases = vec![
        Case {
            id: "a-1".into(),
            task: "course_preview".into(),
            input: serde_json::json!({"topic": "Math"}),
            rubric: "Math intro".into(),
            checks: vec![Check {
                kind: "json".into(),
                value: None,
                critical: false,
            }],
        },
        Case {
            id: "a-2".into(),
            task: "course_preview".into(),
            input: serde_json::json!({"topic": "Science"}),
            rubric: "Science intro".into(),
            checks: vec![Check {
                kind: "json".into(),
                value: None,
                critical: false,
            }],
        },
        Case {
            id: "b-1".into(),
            task: "lesson_test".into(),
            input: serde_json::json!({"topic": "History"}),
            rubric: "History test".into(),
            checks: vec![Check {
                kind: "json".into(),
                value: None,
                critical: false,
            }],
        },
    ];

    let mut thresholds = Thresholds::default();
    thresholds.tasks.insert("course_preview".into(), 1.0);
    thresholds.tasks.insert("lesson_test".into(), 1.0);

    let runner = Runner::mock_with_judge_judgments(vec![
        ("a-1".into(), "PASS".into()),
        ("a-2".into(), "PASS".into()),
        ("b-1".into(), "FAIL".into()), // failing judge calibration
    ]);

    let report = runner.run_cases(cases, &thresholds).await.unwrap();
    assert_eq!(report.task_reports["course_preview"].score, 1.0);
    assert!(report.task_reports["course_preview"].passed);

    assert_eq!(report.task_reports["lesson_test"].score, 0.0);
    assert!(!report.task_reports["lesson_test"].passed);

    // Run must fail overall even though 2/3 (66.7%) passed
    assert!(!report.passed);
}

#[tokio::test]
async fn hf_dataset_double_downloads_and_caches_revision() {
    let mock_server = MockServer::start().await;
    let jsonl_content = r#"{"id":"row-1","task":"preview","input":{"topic":"art"},"rubric":"art rubric","checks":[{"kind":"json"}]}
"#;

    let revision = "0123456789abcdef0123456789abcdef01234567";
    Mock::given(method("GET"))
        .and(path(format!(
            "/datasets/duolify-fixtures/e10-02/resolve/{revision}/golden.jsonl"
        )))
        .respond_with(ResponseTemplate::new(200).set_body_string(jsonl_content))
        .expect(1) // must be fetched exactly once
        .mount(&mock_server)
        .await;

    let temp_cache = std::env::temp_dir().join(format!(
        "hf-cache-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_cache).unwrap();

    let fetched = engine_eval::fetch_dataset(
        &mock_server.uri(),
        "duolify-fixtures/e10-02",
        revision,
        "mock-token",
        &temp_cache,
    )
    .await
    .unwrap();

    assert_eq!(fetched.trim(), jsonl_content.trim());

    // Second fetch should read from cache, making zero HTTP calls
    let cached = engine_eval::fetch_dataset(
        &mock_server.uri(),
        "duolify-fixtures/e10-02",
        revision,
        "mock-token",
        &temp_cache,
    )
    .await
    .unwrap();

    assert_eq!(cached.trim(), jsonl_content.trim());

    let _ = std::fs::remove_dir_all(temp_cache);
}
