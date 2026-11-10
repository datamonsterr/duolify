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

fn regression_cache(label: &str) -> std::path::PathBuf {
    let dir = std::env::temp_dir().join(format!(
        "digest-{label}-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&dir).unwrap();
    dir
}

#[tokio::test]
async fn existing_cache_without_sidecar_is_rejected_and_never_self_signed() {
    let hf = MockServer::start().await;
    let cache = regression_cache("missing-sidecar");
    let stem = format!("duolify-fixtures--e10-04-{PINNED_REVISION}");
    std::fs::write(cache.join(format!("{stem}.jsonl")), VALID_DATASET).unwrap();
    let result = fetch_dataset_with_mirror(
        &hf.uri(),
        None,
        "duolify-fixtures/e10-04",
        PINNED_REVISION,
        "",
        &cache,
    )
    .await;
    assert!(
        result.is_err(),
        "an existing cache must never acquire its own trust anchor"
    );
    assert!(!cache.join(format!("{stem}.sha256")).exists());
    std::fs::remove_dir_all(cache).unwrap();
}

#[tokio::test]
async fn tampered_cache_and_matching_sidecar_are_rejected_against_repo_pin() {
    let hf = MockServer::start().await;
    let cache = regression_cache("forged-sidecar");
    let stem = format!("duolify-fixtures--e10-04-{PINNED_REVISION}");
    let body = VALID_DATASET.replace("Coffee", "Tampered");
    std::fs::write(cache.join(format!("{stem}.jsonl")), &body).unwrap();
    std::fs::write(
        cache.join(format!("{stem}.sha256")),
        engine_eval::compute_sha256(body.as_bytes()),
    )
    .unwrap();
    assert!(
        fetch_dataset_with_mirror(
            &hf.uri(),
            None,
            "duolify-fixtures/e10-04",
            PINNED_REVISION,
            "",
            &cache
        )
        .await
        .is_err()
    );
    std::fs::remove_dir_all(cache).unwrap();
}

#[tokio::test]
async fn mirror_requires_checksum_and_repo_pin_even_when_payload_is_valid_jsonl() {
    for (body, checksum) in [
        (VALID_DATASET.to_string(), None),
        (VALID_DATASET.replace("Coffee", "Tampered"), Some("forged")),
    ] {
        let hf = MockServer::start().await;
        let mirror = MockServer::start().await;
        Mock::given(method("GET"))
            .respond_with(ResponseTemplate::new(503))
            .mount(&hf)
            .await;
        let mut response = ResponseTemplate::new(200).set_body_string(&body);
        if checksum.is_some() {
            response = response.append_header(
                "x-checksum-sha256",
                engine_eval::compute_sha256(body.as_bytes()),
            );
        }
        Mock::given(method("GET"))
            .and(path(format!("/{PINNED_REVISION}/golden.jsonl")))
            .respond_with(response)
            .mount(&mirror)
            .await;
        let cache = regression_cache("mirror");
        assert!(
            fetch_dataset_with_mirror(
                &hf.uri(),
                Some(&format!("{}/{{revision}}/golden.jsonl", mirror.uri())),
                "duolify-fixtures/e10-04",
                PINNED_REVISION,
                "",
                &cache
            )
            .await
            .is_err()
        );
        assert_eq!(std::fs::read_dir(&cache).unwrap().count(), 0);
        std::fs::remove_dir_all(cache).unwrap();
    }
}

#[tokio::test]
async fn hf_client_errors_and_integrity_failures_never_use_mirror() {
    for status in [400, 401, 403, 404, 429, 200] {
        let hf = MockServer::start().await;
        let mirror = MockServer::start().await;
        Mock::given(method("GET"))
            .respond_with(
                ResponseTemplate::new(status)
                    .set_body_string(VALID_DATASET.replace("Coffee", "Tampered")),
            )
            .mount(&hf)
            .await;
        Mock::given(method("GET"))
            .respond_with(
                ResponseTemplate::new(200)
                    .set_body_string(VALID_DATASET)
                    .append_header("x-checksum-sha256", VALID_CHECKSUM),
            )
            .mount(&mirror)
            .await;
        let cache = regression_cache("hf-client-error");
        assert!(
            fetch_dataset_with_mirror(
                &hf.uri(),
                Some(&mirror.uri()),
                "duolify-fixtures/e10-04",
                PINNED_REVISION,
                "",
                &cache
            )
            .await
            .is_err(),
            "HF {status} must fail closed"
        );
        assert!(mirror.received_requests().await.unwrap().is_empty());
        assert_eq!(std::fs::read_dir(&cache).unwrap().count(), 0);
        std::fs::remove_dir_all(cache).unwrap();
    }
}

#[tokio::test]
async fn unknown_dataset_or_revision_fails_without_remote_access() {
    let hf = MockServer::start().await;
    let cache = regression_cache("unknown-pin");
    for (dataset, revision) in [
        ("unknown/dataset", PINNED_REVISION),
        (
            "duolify-fixtures/e10-04",
            "ffffffffffffffffffffffffffffffffffffffff",
        ),
    ] {
        let err = fetch_dataset_with_mirror(&hf.uri(), None, dataset, revision, "", &cache)
            .await
            .unwrap_err();
        assert!(err.to_string().contains("pinned digest"), "{err}");
    }
    assert!(hf.received_requests().await.unwrap().is_empty());
    std::fs::remove_dir_all(cache).unwrap();
}

#[tokio::test]
async fn network_outage_and_http_timeout_recover_with_sidecar_only_mirror() {
    for status in [None, Some(408)] {
        let hf = MockServer::start().await;
        if let Some(status) = status {
            Mock::given(method("GET"))
                .respond_with(ResponseTemplate::new(status))
                .mount(&hf)
                .await;
        }
        let mirror = MockServer::start().await;
        let object_path = format!("/{PINNED_REVISION}/golden.jsonl");
        Mock::given(method("GET"))
            .and(path(&object_path))
            .respond_with(ResponseTemplate::new(200).set_body_string(VALID_DATASET))
            .mount(&mirror)
            .await;
        Mock::given(method("GET"))
            .and(path(format!("{object_path}.sha256")))
            .respond_with(ResponseTemplate::new(200).set_body_string(VALID_CHECKSUM))
            .mount(&mirror)
            .await;
        let cache = regression_cache("outage-sidecar-only");
        let base_url = if status.is_none() {
            // Wiremock pools listeners; use a closed local socket for connection refusal.
            let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
            let url = format!("http://{}", listener.local_addr().unwrap());
            drop(listener);
            url
        } else {
            hf.uri()
        };
        let fetched = fetch_dataset_with_mirror(
            &base_url,
            Some(&format!("{}/{{revision}}/golden.jsonl", mirror.uri())),
            "duolify-fixtures/e10-04",
            PINNED_REVISION,
            "",
            &cache,
        )
        .await
        .unwrap();
        assert_eq!(fetched.source, "mirror");
        assert!(fetched.verified);
        assert_eq!(fetched.checksum, VALID_CHECKSUM);
        drop(hf);
        std::fs::remove_dir_all(cache).unwrap();
    }
}

#[tokio::test]
async fn fake_hf_endpoint_downloads_computes_checksum_and_caches() {
    let mock_hf = MockServer::start().await;

    Mock::given(method("GET"))
        .and(path(format!(
            "/datasets/duolify-fixtures/e10-04/resolve/{PINNED_REVISION}/golden.jsonl"
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
        "duolify-fixtures/e10-04",
        PINNED_REVISION,
        "mock-token",
        &temp_cache,
    )
    .await
    .unwrap();

    assert_eq!(fetched.content.trim(), VALID_DATASET.trim());
    assert_eq!(fetched.source, "hf");
    assert!(fetched.verified);
    assert_eq!(fetched.revision, PINNED_REVISION);
    assert_eq!(fetched.checksum, VALID_CHECKSUM);

    // Verify cache file and checksum file were written to disk
    let cache_file = temp_cache.join(format!("duolify-fixtures--e10-04-{PINNED_REVISION}.jsonl"));
    let checksum_file =
        temp_cache.join(format!("duolify-fixtures--e10-04-{PINNED_REVISION}.sha256"));
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
        "duolify-fixtures/e10-04",
        PINNED_REVISION,
        "mock-token",
        &temp_cache,
    )
    .await
    .unwrap();

    assert_eq!(cached.content.trim(), VALID_DATASET.trim());
    assert_eq!(cached.source, "cache");
    assert!(cached.verified);
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
            "/datasets/duolify-fixtures/e10-04/resolve/{PINNED_REVISION}/golden.jsonl"
        )))
        .respond_with(ResponseTemplate::new(503))
        .mount(&mock_hf)
        .await;

    // RustFS mirror endpoint: serves the pinned dataset
    let mirror_path = format!("/mirror/duolify-fixtures/e10-04/{PINNED_REVISION}/golden.jsonl");
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
        "duolify-fixtures/e10-04",
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

    let cache_file = temp_cache.join(format!("duolify-fixtures--e10-04-{PINNED_REVISION}.jsonl"));
    let checksum_file =
        temp_cache.join(format!("duolify-fixtures--e10-04-{PINNED_REVISION}.sha256"));

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
        "duolify-fixtures/e10-04",
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
    let mirror_path = format!("/mirror/duolify-fixtures/e10-04/{PINNED_REVISION}/golden.jsonl");
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
        "duolify-fixtures/e10-04",
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
        "duolify-fixtures/e10-04",
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
