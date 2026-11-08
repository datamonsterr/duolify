use super::*;

#[test]
fn config_has_all_tasks() {
    let config: Models = toml::from_str(include_str!("../../../config/models.toml")).unwrap();
    assert_eq!(config.tasks.len(), 5);
    assert_eq!(config.tasks["chat"].model, "claude-haiku-5-5");
}

#[test]
fn slide_rejects_invalid_documents() {
    assert!(slide::validate("<div id=\"root\">hello</div>", &[]).is_ok());
    assert!(slide::validate("<div>hello</div>", &[]).is_err());
    assert!(
        slide::validate(
            "<div id=\"root\"><img src=\"https://evil.test/x\"></div>",
            &[]
        )
        .is_err()
    );
    assert!(
        slide::validate(
            "<div id=\"root\"><img src=\"https://safe.test/x\"></div>",
            &["safe.test"]
        )
        .is_ok()
    );
    assert!(
        slide::validate(
            &format!("<div id=\"root\">{}</div>", "x".repeat(slide::MAX_BYTES)),
            &[]
        )
        .is_err()
    );
}
