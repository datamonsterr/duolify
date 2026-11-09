use super::*;

#[test]
fn closed_schema_is_shared_and_rejects_missing_unknown_and_oversized_fields() {
    let schema: serde_json::Value = serde_json::from_str(SCHEMA).unwrap();
    assert_eq!(schema["additionalProperties"], false);
    assert!(!valid_spec("{}"));
    assert!(!valid_spec(&"x".repeat(8193)));
    let original = include_str!(
        "../../../../../services/api/internal/characters/schema/original.fixture.json"
    );
    assert!(valid_spec(original));
    let mut value: serde_json::Value = serde_json::from_str(original).unwrap();
    value["info"]["name"] = serde_json::json!("🌱".repeat(40));
    assert!(valid_spec(&value.to_string()));
    value["appearance"]["palette"]["outline"] = value["appearance"]["palette"]["body"].clone();
    assert!(!valid_spec(&value.to_string()));
}

#[test]
fn screening_verdicts_fail_closed_and_use_only_bounded_reasons() {
    assert_eq!(
        verdict(r#"{"approved":true,"reason_code":""}"#),
        Some((true, String::new()))
    );
    assert_eq!(
        verdict(r#"{"approved":false,"reason_code":"real_person"}"#),
        Some((false, "real_person".into()))
    );
    assert!(verdict(r#"{"approved":true,"reason_code":"unsafe"}"#).is_none());
    assert!(verdict(r#"{"approved":false,"reason_code":"learner text"}"#).is_none());
    assert!(verdict("{}").is_none());
}
