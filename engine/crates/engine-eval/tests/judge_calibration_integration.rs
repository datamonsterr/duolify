use engine_eval::calibration::{Degradation, anchors, degrade};
use serde_json::json;

#[test]
fn degraders_change_the_relevant_content_without_changing_the_original() {
    let lesson = json!({"slides":[{"html":"<p>HOOK</p>","narration":"Different words"},{"html":"<p>IDEA</p>","narration":"Idea"},{"html":"<p>EXAMPLE</p>","narration":"Example"},{"html":"<p>SUMMARY</p>","narration":"Remember"}], "test":[{"choices":["yes","no"],"answer_index":0}]});
    let copy = lesson.clone();
    let long = degrade(&lesson, Degradation::LongSlide);
    assert_eq!(lesson, copy);
    assert_eq!(
        engine_eval::rubric::visible_text(long["slides"][0]["html"].as_str().unwrap())
            .split_whitespace()
            .count(),
        80
    );
    let repeated = degrade(&lesson, Degradation::CopyNarration);
    assert_eq!(repeated["slides"][0]["narration"], "HOOK");
    let absurd = degrade(&lesson, Degradation::AbsurdDistractors);
    assert_eq!(absurd["test"][0]["choices"][0], "yes");
    assert_ne!(absurd["test"][0]["choices"][1], "no");
    let without_example = degrade(&lesson, Degradation::WorkedExample);
    assert!(!without_example.to_string().contains("EXAMPLE"));
    let without_summary = degrade(&lesson, Degradation::Summary);
    assert!(!without_summary.to_string().contains("SUMMARY"));
}

#[test]
fn review_anchors_have_positive_and_negative_examples() {
    let all = anchors().unwrap();
    let r8: Vec<_> = all.iter().filter(|a| a.criterion == "R8").collect();
    assert!(r8.len() >= 20);
    assert!(r8.iter().any(|a| a.expected));
    assert!(r8.iter().any(|a| !a.expected));
}
