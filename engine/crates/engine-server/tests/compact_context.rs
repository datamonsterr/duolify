//! Task 2.4: the compact JSON rendering of a lesson request against the prost `Debug` text it
//! replaces for routed prompts (token estimate: 4 characters each).
use engine_core::skills::estimate_tokens;
use engine_proto as pb;

#[test]
fn compact_json_is_much_smaller_than_the_debug_rendering() {
    let req = pb::GenerateLessonRequest {
        outline: Some(pb::CourseOutline {
            title: "Go for beginners".into(),
            ..Default::default()
        }),
        lesson: Some(pb::LessonRef {
            lesson_id: "l-1".into(),
            title: "Slices".into(),
            goal: "Explain slices share a backing array".into(),
            plan: Some(pb::LessonPlan {
                objective: "Compare slices with arrays".into(),
                ..Default::default()
            }),
        }),
        learner: Some(pb::LearnerProfile {
            locale: "en".into(),
            ..Default::default()
        }),
        limits: Some(pb::UsageLimits {
            input_tokens: 9000,
            output_tokens: 6000,
            model: "tencent/hy4-flash".into(),
            ..Default::default()
        }),
        ..Default::default()
    };
    let debug = format!("{req:?}");
    let compact = engine_core::lessontest::compact_context(&serde_json::to_value(&req).unwrap());
    assert!(compact.contains("Slices") && compact.contains("Go for beginners"));
    assert!(compact.contains("Compare slices with arrays"));
    assert!(!compact.contains("limits") && !compact.contains("hy4"));
    let (d, c) = (estimate_tokens(&debug), estimate_tokens(&compact));
    eprintln!("debug {d} tokens, compact {c} tokens");
    assert!(c * 10 < d * 6, "compact {c} vs debug {d}");
}
