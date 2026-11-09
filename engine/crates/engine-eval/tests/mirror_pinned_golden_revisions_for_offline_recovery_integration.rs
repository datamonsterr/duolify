use engine_eval::fetch_dataset_with_mirror;
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
async fn fake_hf_endpoint_downloads_computes_checksum_and_caches() {
    let mock_hf = MockServer::start().await;

    Mock::given(method("GET"))
        .and(path(format!(
            "/datasets/datamonsterr/duolify-golden/resolve/{PINNED_REVISION}/golden.jsonl"
        )))
        .respond_with(ResponseTemplate::new(200).set_body_string(VALID_DATASET))
        .expect(1) // must be fetched once from HF
        .mount(&mock_hf)
        .await;

    let temp_cache = std::env::temp_dir().join(format!(
        "hf-cache-int-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_cache).unwrap();

    let fetched = fetch_dataset_with_mirror(
        &mock_hf.uri(),
        None,
        "datamonsterr/duolify-golden",
        PINNED_REVISION,
        "mock-token",
        &temp_cache,
    )
    .await
    .unwrap();

    assert_eq!(fetched.content.trim(), VALID_DATASET.trim());
    assert_eq!(fetched.source, "hf");
    assert_eq!(fetched.revision, PINNED_REVISION);
    assert_eq!(fetched.checksum, VALID_CHECKSUM);

    // Verify cache file and checksum file were written to disk
    let cache_file = temp_cache.join(format!(
        "datamonsterr--duolify-golden-{PINNED_REVISION}.jsonl"
    ));
    let checksum_file = temp_cache.join(format!(
        "datamonsterr--duolify-golden-{PINNED_REVISION}.sha256"
    ));
    assert!(cache_file.is_file(), "cache file must exist");
    assert!(checksum_file.is_file(), "checksum file must exist");
    assert_eq!(
        std::fs::read_to_string(&checksum_file).unwrap().trim(),
        VALID_CHECKSUM
    );

    // Second fetch should read from cache, making zero calls to HF
    let cached = fetch_dataset_with_mirror(
        &mock_hf.uri(),
        None,
        "datamonsterr/duolify-golden",
        PINNED_REVISION,
        "mock-token",
        &temp_cache,
    )
    .await
    .unwrap();

    assert_eq!(cached.content.trim(), VALID_DATASET.trim());
    assert_eq!(cached.source, "cache");
    assert_eq!(cached.checksum, VALID_CHECKSUM);

    let _ = std::fs::remove_dir_all(temp_cache);
}

#[tokio::test]
async fn hf_outage_recovers_from_rustfs_mirror_and_verifies_checksum() {
    let mock_hf = MockServer::start().await;
    let mock_mirror = MockServer::start().await;

    // HF outage: returns 503 Service Unavailable
    Mock::given(method("GET"))
        .and(path(format!(
            "/datasets/datamonsterr/duolify-golden/resolve/{PINNED_REVISION}/golden.jsonl"
        )))
        .respond_with(ResponseTemplate::new(503))
        .mount(&mock_hf)
        .await;

    // RustFS mirror endpoint: serves the pinned dataset
    let mirror_path = format!("/mirror/datamonsterr/duolify-golden/{PINNED_REVISION}/golden.jsonl");
    Mock::given(method("GET"))
        .and(path(&mirror_path))
        .respond_with(
            ResponseTemplate::new(200)
                .set_body_string(VALID_DATASET)
                .append_header("x-checksum-sha256", VALID_CHECKSUM),
        )
        .expect(1)
        .mount(&mock_mirror)
        .await;

    let temp_cache = std::env::temp_dir().join(format!(
        "mirror-cache-int-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_cache).unwrap();

    let fetched = fetch_dataset_with_mirror(
        &mock_hf.uri(),
        Some(&format!("{}/mirror", mock_mirror.uri())),
        "datamonsterr/duolify-golden",
        PINNED_REVISION,
        "mock-token",
        &temp_cache,
    )
    .await
    .unwrap();

    assert_eq!(fetched.content.trim(), VALID_DATASET.trim());
    assert_eq!(fetched.source, "mirror");
    assert_eq!(fetched.revision, PINNED_REVISION);
    assert_eq!(fetched.checksum, VALID_CHECKSUM);

    let _ = std::fs::remove_dir_all(temp_cache);
}

#[tokio::test]
async fn cache_corruption_is_detected_and_rejected() {
    let temp_cache = std::env::temp_dir().join(format!(
        "corrupt-cache-int-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_cache).unwrap();

    let cache_file = temp_cache.join(format!(
        "datamonsterr--duolify-golden-{PINNED_REVISION}.jsonl"
    ));
    let checksum_file = temp_cache.join(format!(
        "datamonsterr--duolify-golden-{PINNED_REVISION}.sha256"
    ));

    // Write corrupted content, but the checksum file expects VALID_CHECKSUM
    std::fs::write(&cache_file, "TAMPERED CORRUPTED DATA").unwrap();
    std::fs::write(&checksum_file, VALID_CHECKSUM).unwrap();

    let mock_hf = MockServer::start().await;
    // HF is down
    Mock::given(method("GET"))
        .respond_with(ResponseTemplate::new(500))
        .mount(&mock_hf)
        .await;

    let res = fetch_dataset_with_mirror(
        &mock_hf.uri(),
        None,
        "datamonsterr/duolify-golden",
        PINNED_REVISION,
        "mock-token",
        &temp_cache,
    )
    .await;

    assert!(
        res.is_err(),
        "expected error due to cache corruption, got ok"
    );
    let err = res.unwrap_err().to_string();
    assert!(
        err.contains("corruption") || err.contains("checksum"),
        "error message should indicate cache corruption, got: {err}"
    );

    let _ = std::fs::remove_dir_all(temp_cache);
}

#[tokio::test]
async fn mirror_checksum_mismatch_rejects_corrupted_mirror() {
    let mock_hf = MockServer::start().await;
    let mock_mirror = MockServer::start().await;

    // HF outage
    Mock::given(method("GET"))
        .respond_with(ResponseTemplate::new(500))
        .mount(&mock_hf)
        .await;

    // Mirror returns corrupted payload with bad checksum
    let mirror_path = format!("/mirror/datamonsterr/duolify-golden/{PINNED_REVISION}/golden.jsonl");
    Mock::given(method("GET"))
        .and(path(&mirror_path))
        .respond_with(
            ResponseTemplate::new(200)
                .set_body_string("tampered payload")
                .append_header("x-checksum-sha256", VALID_CHECKSUM), // advertised hash does not match payload!
        )
        .mount(&mock_mirror)
        .await;

    let temp_cache = std::env::temp_dir().join(format!(
        "corrupt-mirror-int-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_cache).unwrap();

    let res = fetch_dataset_with_mirror(
        &mock_hf.uri(),
        Some(&format!("{}/mirror", mock_mirror.uri())),
        "datamonsterr/duolify-golden",
        PINNED_REVISION,
        "mock-token",
        &temp_cache,
    )
    .await;

    assert!(
        res.is_err(),
        "expected error due to mirror checksum mismatch"
    );
    let err = res.unwrap_err().to_string();
    assert!(
        err.contains("checksum") || err.contains("corrupt"),
        "expected checksum mismatch error, got: {err}"
    );

    let _ = std::fs::remove_dir_all(temp_cache);
}

#[tokio::test]
async fn unpinned_revision_fails_explicitly_without_network_request() {
    let temp_cache = std::env::temp_dir().join(format!(
        "unpinned-int-{}",
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&temp_cache).unwrap();

    let res = fetch_dataset_with_mirror(
        "http://invalid.localhost:9999",
        None,
        "datamonsterr/duolify-golden",
        "main", // branch name, not a 40-char hex commit!
        "mock-token",
        &temp_cache,
    )
    .await;

    assert!(res.is_err(), "expected error for unpinned branch revision");
    let err = res.unwrap_err().to_string();
    assert!(
        err.contains("pinned") || err.contains("commit") || err.contains("revision"),
        "expected error to mention pinned commit hash requirement, got: {err}"
    );

    let _ = std::fs::remove_dir_all(temp_cache);
}
