use super::Correlation;

#[test]
fn validates_correlation_without_retaining_private_metadata() {
    let traceparent = "00-11111111111111111111111111111111-2222222222222222-01";
    let value = Correlation::parse("ABCDEFGHIJKLMNOPQRSTUVWXYZ", traceparent).unwrap();
    assert_eq!(value.request_id, "ABCDEFGHIJKLMNOPQRSTUVWXYZ");
    assert_eq!(value.trace_id, "11111111111111111111111111111111");
    assert_eq!(value.parent_span_id, "2222222222222222");
    for id in [
        "Bearer private-token",
        "raw prompt",
        "private\ntext",
        "",
        &"x".repeat(200),
    ] {
        assert!(Correlation::parse(id, traceparent).is_none());
    }
    for parent in [
        "private-text",
        "00-00000000000000000000000000000000-2222222222222222-01",
        "00-11111111111111111111111111111111-0000000000000000-01",
        "00-11111111111111111111111111111111-2222222222222222-zz",
    ] {
        assert!(Correlation::parse("ABCDEFGHIJKLMNOPQRSTUVWXYZ", parent).is_none());
    }
}
