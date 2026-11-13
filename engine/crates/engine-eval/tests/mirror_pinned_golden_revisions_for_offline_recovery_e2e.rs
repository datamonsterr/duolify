mod common;

use std::process::Command;
use wiremock::{
    Mock, MockServer, ResponseTemplate,
    matchers::{method, path},
};

const VALID_DATASET: &str = r#"{"id":"preview-1","task":"course_preview","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
{"id":"outline-1","task":"course_outline","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
{"id":"lesson-1","task":"lesson_slides","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
{"id":"test-1","task":"lesson_test","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
{"id":"chat-1","task":"tutor_chat","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
{"id":"note-1","task":"note_enrich","input":{"topic":"Coffee"},"rubric":"Pass","checks":[{"kind":"json"}]}
"#;

const VALID_CHECKSUM: &str = "e5bcda9d5a4b84a383f24271af9f763ffb38dc51625ec2e1a00daa3feb947f89";
const PINNED_REVISION: &str = "0123456789abcdef0123456789abcdef01234567";

#[tokio::test]
async fn run_pinned_evaluation_through_hf_outage_with_valid_mirror_succeeds() {
    let hf_server = MockServer::start().await;
    let mirror_server = MockServer::start().await;

    // HF outage: returns 500 Internal Server Error
    Mock::given(method("GET"))
        .and(path(format!(
            "/datasets/duolify-fixtures/e10-04/resolve/{PINNED_REVISION}/golden.jsonl"
        )))
        .respond_with(ResponseTemplate::new(500))
        .mount(&hf_server)
        .await;

    // Mirror succeeds: returns valid dataset and checksum header / checksum file
    let mirror_dataset_path =
        format!("/mirror/duolify-fixtures/e10-04/{PINNED_REVISION}/golden.jsonl");
    let mirror_checksum_path = format!("{mirror_dataset_path}.sha256");

    Mock::given(method("GET"))
        .and(path(&mirror_dataset_path))
        .respond_with(
            ResponseTemplate::new(200)
                .set_body_string(VALID_DATASET)
                .append_header("x-checksum-sha256", VALID_CHECKSUM),
        )
        .mount(&mirror_server)
        .await;

    Mock::given(method("GET"))
        .and(path(&mirror_checksum_path))
        .respond_with(ResponseTemplate::new(200).set_body_string(VALID_CHECKSUM))
        .mount(&mirror_server)
        .await;

    let temp_dir = std::env::temp_dir().join(format!(
        "eval-e2e-mirror-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_dir).unwrap();

    let thresholds_file = temp_dir.join("thresholds.toml");
    let reports_dir = temp_dir.join("reports");
    let cache_dir = temp_dir.join("cache");

    std::fs::write(
        &thresholds_file,
        r#"minimum_score = 1.0
[tasks]
course_preview = 1.0
course_outline = 1.0
lesson_slides = 1.0
lesson_test = 1.0
tutor_chat = 1.0
note_enrich = 1.0
"#,
    )
    .unwrap();

    let bin = common::eval_bin();
    let output = Command::new(bin)
        .arg("--provider")
        .arg("mock")
        .arg("--thresholds")
        .arg(&thresholds_file)
        .arg("--reports")
        .arg(&reports_dir)
        .env("HF_BASE_URL", hf_server.uri())
        .env(
            "HF_GOLDEN_MIRROR_URL",
            format!("{}/mirror", mirror_server.uri()),
        )
        .env("HF_TOKEN", "mock-hf-token")
        .env("HF_GOLDEN_DATASET", "duolify-fixtures/e10-04")
        .env("HF_GOLDEN_REVISION", PINNED_REVISION)
        .env("EVAL_CACHE_DIR", &cache_dir)
        .output()
        .expect("failed to execute duolify-eval");

    let stdout = String::from_utf8_lossy(&output.stdout);
    let stderr = String::from_utf8_lossy(&output.stderr);

    assert!(
        output.status.success(),
        "eval failed during mirror recovery. stdout: {stdout}, stderr: {stderr}"
    );

    // Verify reports exist and record mirror source and pinned revision
    let mut json_reports = Vec::new();
    for entry in std::fs::read_dir(&reports_dir).unwrap() {
        let path = entry.unwrap().path();
        if path.extension().and_then(|s| s.to_str()) == Some("json") {
            json_reports.push(path);
        }
    }
    assert_eq!(json_reports.len(), 1, "expected 1 json report generated");

    let report_content = std::fs::read_to_string(&json_reports[0]).unwrap();
    let report: serde_json::Value = serde_json::from_str(&report_content).unwrap();
    assert_eq!(report["dataset_revision"], PINNED_REVISION);
    assert_eq!(report["dataset_source"], "mirror");
    assert_eq!(report["dataset_verified"], true);
    assert_eq!(report["passed"], true);

    let _ = std::fs::remove_dir_all(temp_dir);
}

#[tokio::test]
async fn unsafe_recovery_inputs_never_produce_a_passing_report() {
    for scenario in ["missing-checksum", "forged-checksum", "401", "403", "404"] {
        let hf = MockServer::start().await;
        let mirror = MockServer::start().await;
        let status = scenario.parse::<u16>().unwrap_or(503);
        Mock::given(method("GET"))
            .respond_with(ResponseTemplate::new(status))
            .mount(&hf)
            .await;
        let body = if scenario == "forged-checksum" {
            VALID_DATASET.replace("Coffee", "Tampered")
        } else {
            VALID_DATASET.to_string()
        };
        let mut response = ResponseTemplate::new(200).set_body_string(&body);
        if scenario != "missing-checksum" {
            response = response.append_header(
                "x-checksum-sha256",
                engine_eval::compute_sha256(body.as_bytes()),
            );
        }
        Mock::given(method("GET"))
            .and(path(format!("/mirror/{PINNED_REVISION}/golden.jsonl")))
            .respond_with(response)
            .mount(&mirror)
            .await;
        let temp = std::env::temp_dir().join(format!(
            "eval-unsafe-{scenario}-{}-{}",
            std::process::id(),
            hf.address().port()
        ));
        let output = Command::new(common::eval_bin())
            .args(["--provider", "mock", "--reports"])
            .arg(temp.join("reports"))
            .env("HF_BASE_URL", hf.uri())
            .env(
                "HF_GOLDEN_MIRROR_URL",
                format!("{}/mirror/{{revision}}/golden.jsonl", mirror.uri()),
            )
            .env("HF_TOKEN", "fake-token")
            .env("HF_GOLDEN_DATASET", "duolify-fixtures/e10-04")
            .env("HF_GOLDEN_REVISION", PINNED_REVISION)
            .env("EVAL_CACHE_DIR", temp.join("cache"))
            .output()
            .unwrap();
        assert!(
            !output.status.success(),
            "{scenario} was accepted: {}",
            String::from_utf8_lossy(&output.stdout)
        );
        assert!(!temp.join("reports/latest.json").exists());
        if status < 500 {
            assert!(
                mirror.received_requests().await.unwrap().is_empty(),
                "{status} must not access mirror"
            );
        }
        let _ = std::fs::remove_dir_all(temp);
    }
}

#[tokio::test]
async fn run_pinned_evaluation_through_hf_outage_with_corrupt_mirror_fails_explicitly() {
    let hf_server = MockServer::start().await;
    let mirror_server = MockServer::start().await;

    // HF outage: returns 500
    Mock::given(method("GET"))
        .and(path(format!(
            "/datasets/duolify-fixtures/e10-04/resolve/{PINNED_REVISION}/golden.jsonl"
        )))
        .respond_with(ResponseTemplate::new(500))
        .mount(&hf_server)
        .await;

    // Mirror returns corrupted payload with bad checksum
    let mirror_dataset_path =
        format!("/mirror/duolify-fixtures/e10-04/{PINNED_REVISION}/golden.jsonl");
    let mirror_checksum_path = format!("{mirror_dataset_path}.sha256");

    Mock::given(method("GET"))
        .and(path(&mirror_dataset_path))
        .respond_with(
            ResponseTemplate::new(200)
                .set_body_string("corrupted non-jsonl garbage content")
                .append_header(
                    "x-checksum-sha256",
                    "0000000000000000000000000000000000000000000000000000000000000000",
                ),
        )
        .mount(&mirror_server)
        .await;

    Mock::given(method("GET"))
        .and(path(&mirror_checksum_path))
        .respond_with(
            ResponseTemplate::new(200).set_body_string(
                "0000000000000000000000000000000000000000000000000000000000000000",
            ),
        )
        .mount(&mirror_server)
        .await;

    let temp_dir = std::env::temp_dir().join(format!(
        "eval-e2e-corrupt-mirror-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_dir).unwrap();

    let thresholds_file = temp_dir.join("thresholds.toml");
    let reports_dir = temp_dir.join("reports");
    let cache_dir = temp_dir.join("cache");

    std::fs::write(&thresholds_file, "minimum_score = 1.0\n").unwrap();

    let bin = common::eval_bin();
    let output = Command::new(bin)
        .arg("--provider")
        .arg("mock")
        .arg("--thresholds")
        .arg(&thresholds_file)
        .arg("--reports")
        .arg(&reports_dir)
        .env("HF_BASE_URL", hf_server.uri())
        .env(
            "HF_GOLDEN_MIRROR_URL",
            format!("{}/mirror", mirror_server.uri()),
        )
        .env("HF_TOKEN", "mock-hf-token")
        .env("HF_GOLDEN_DATASET", "duolify-fixtures/e10-04")
        .env("HF_GOLDEN_REVISION", PINNED_REVISION)
        .env("EVAL_CACHE_DIR", &cache_dir)
        .output()
        .expect("failed to execute duolify-eval");

    assert!(
        !output.status.success(),
        "expected failure due to corrupt mirror dataset, but process succeeded! stdout: {}, stderr: {}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );

    let _ = std::fs::remove_dir_all(temp_dir);
}

#[test]
fn unpinned_revision_or_missing_mirror_fails_explicitly_without_silent_mock_substitute() {
    let temp_dir = std::env::temp_dir().join(format!(
        "eval-e2e-unpinned-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_dir).unwrap();
    let reports_dir = temp_dir.join("reports");

    let bin = common::eval_bin();
    // Request a live eval with branch name "main" (not an immutable 40-char hex commit hash)
    let output = Command::new(bin)
        .arg("--provider")
        .arg("mock")
        .arg("--reports")
        .arg(&reports_dir)
        .env("HF_TOKEN", "mock-token")
        .env("HF_GOLDEN_DATASET", "duolify-fixtures/e10-04")
        .env("HF_GOLDEN_REVISION", "main") // unpinned branch!
        .output()
        .expect("failed to execute duolify-eval");

    assert!(
        !output.status.success(),
        "expected duolify-eval to fail explicitly when unpinned revision is passed, but succeeded. stdout: {}, stderr: {}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );

    let err_msg = format!(
        "{}\n{}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );
    assert!(
        err_msg.contains("pinned") || err_msg.contains("commit") || err_msg.contains("revision"),
        "expected error message to mention pinned commit hash requirement, got: {err_msg}"
    );

    let _ = std::fs::remove_dir_all(temp_dir);
}
