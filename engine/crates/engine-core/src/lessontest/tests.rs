use super::*;

#[test]
fn test_validate_slide_valid() {
    let html = r#"<div id="root"><h1>Constellations</h1><p>Learn coordinates.</p></div>"#;
    let res = validate_slide(html, 3000, "Learn coordinates in the night sky.");
    assert!(res.is_ok());
}

#[test]
fn test_validate_slide_invalid_html() {
    let html = r#"<div>missing root</div>"#;
    let res = validate_slide(html, 3000, "Narration");
    assert!(res.is_err());
}

#[test]
fn test_validate_slide_duration_out_of_bounds() {
    let html = r#"<div id="root">Valid</div>"#;
    assert!(validate_slide(html, 500, "Narration").is_err());
    assert!(validate_slide(html, 100_000, "Narration").is_err());
}

#[test]
fn test_validate_narration_markup_rejected() {
    assert!(validate_narration("<b>Hello</b> world", 3000).is_err());
    assert!(validate_narration("Plain spoken text.", 3000).is_ok());
}

#[test]
fn test_validate_narration_empty_rejected() {
    assert!(validate_narration("   ", 3000).is_err());
}

#[test]
fn test_validate_question_valid() {
    let choices = vec![
        "Earth".to_string(),
        "Mars".to_string(),
        "Jupiter".to_string(),
    ];
    let res = validate_question(
        "Which planet do we live on?",
        &choices,
        0,
        "Earth is our home planet.",
    );
    assert!(res.is_ok());
}

#[test]
fn test_validate_question_duplicate_choices() {
    let choices = vec!["Earth".to_string(), "earth".to_string()];
    let res = validate_question("Prompt", &choices, 0, "Exp");
    assert!(res.is_err());
}

#[test]
fn test_validate_question_answer_index_out_of_bounds() {
    let choices = vec!["A".to_string(), "B".to_string()];
    let res = validate_question("Prompt", &choices, 2, "Exp");
    assert!(res.is_err());
}

#[test]
fn test_validate_question_fewer_than_two_choices() {
    let choices = vec!["Solo".to_string()];
    let res = validate_question("Prompt", &choices, 0, "Exp");
    assert!(res.is_err());
}

#[test]
fn test_validate_lesson_full() {
    let valid_json = serde_json::json!({
        "slides": [
            {
                "html": "<div id=\"root\"><h1>Sky</h1></div>",
                "duration_ms": 3000,
                "narration": "Looking at the sky.",
                "topic_ids": ["top-sky"]
            }
        ],
        "test": [
            {
                "topic_id": "top-sky",
                "prompt": "What is in the sky?",
                "choices": ["Stars", "Trees"],
                "answer_index": 0,
                "explanation": "Stars are in the sky."
            }
        ],
        "variant_pool": [
            {
                "topic_id": "top-sky",
                "variants": [
                    {
                        "topic_id": "top-sky",
                        "variant_of": "q1",
                        "prompt": "Which of these appears overhead in the night sky?",
                        "choices": ["Trees", "Stars"],
                        "answer_index": 1,
                        "explanation": "Stars are visible overhead."
                    },
                    {
                        "topic_id": "top-sky",
                        "variant_of": "q1",
                        "prompt": "When looking up on a clear night, what do you see?",
                        "choices": ["Luminous celestial bodies (stars)", "Forests"],
                        "answer_index": 0,
                        "explanation": "Stars shine at night."
                    }
                ]
            }
        ]
    });
    assert!(validate_lesson(&valid_json).is_ok());
}

#[test]
fn test_validate_slide_missing_topic_ids_rejected() {
    let slide = serde_json::json!({
        "html": "<div id=\"root\"><h1>Sky</h1></div>",
        "duration_ms": 3000,
        "narration": "Looking at the sky.",
        "topic_ids": []
    });
    let lesson = serde_json::json!({
        "slides": [slide],
        "test": [
            {
                "topic_id": "top-sky",
                "prompt": "Question?",
                "choices": ["A", "B"],
                "answer_index": 0,
                "explanation": "Exp"
            }
        ]
    });
    assert!(validate_lesson(&lesson).is_err());
}

#[test]
fn test_validate_question_untaught_topic_rejected() {
    let lesson = serde_json::json!({
        "slides": [
            {
                "html": "<div id=\"root\"><h1>Sky</h1></div>",
                "duration_ms": 3000,
                "narration": "Looking at the sky.",
                "topic_ids": ["top-sky"]
            }
        ],
        "test": [
            {
                "topic_id": "top-geology",
                "prompt": "What is granite?",
                "choices": ["Rock", "Gas"],
                "answer_index": 0,
                "explanation": "Exp"
            }
        ]
    });
    assert!(validate_lesson(&lesson).is_err());
}

#[test]
fn test_validate_variant_unchanged_prompt_rejected() {
    let source_prompt = "Why does the Sun rise in the east?";
    let choices = vec!["A".to_string(), "B".to_string()];
    let res = validate_variant(
        "top-sun",
        "q1",
        source_prompt,
        &choices,
        0,
        source_prompt, // Same prompt!
        &["B".to_string(), "A".to_string()],
        1,
        "Exp",
        &[],
    );
    assert!(res.is_err());
}

#[test]
fn test_validate_variant_duplicate_choices_rejected() {
    let res = validate_variant(
        "top-sun",
        "q1",
        "Why does the Sun rise in the east?",
        &["A".to_string(), "B".to_string()],
        0,
        "Paraphrased question about sunrise?",
        &["Earth".to_string(), "earth".to_string()],
        0,
        "Exp",
        &[],
    );
    assert!(res.is_err());
}

#[test]
fn test_validate_history_valid() {
    let hist = vec![
        "Lesson 1: Sky Coordinates. Passed. Weak topics: celestial-altitude.".to_string(),
        "Lesson 2: Star Navigation. Passed.".to_string(),
    ];
    assert!(validate_history(&hist).is_ok());
}

#[test]
fn test_validate_history_empty_item_rejected() {
    let hist = vec!["Lesson 1: Sky Coordinates.".to_string(), "   ".to_string()];
    assert!(validate_history(&hist).is_err());
}

#[test]
fn test_validate_history_empty_slice_valid() {
    assert!(validate_history(&[]).is_ok());
}

#[test]
fn test_reorder_choices_invariance() {
    let choices = vec![
        "Earth spins west to east".to_string(),
        "The Sun orbits Earth daily".to_string(),
        "The Moon pushes it up".to_string(),
    ];
    let (reordered, new_idx) = reorder_choices(&choices, 0).expect("reorder success");
    assert_ne!(choices, reordered);
    assert_eq!(reordered[new_idx as usize], choices[0]);
    assert!(validate_exhausted_fallback_variant(&choices, 0, &reordered, new_idx).is_ok());
}

#[test]
fn test_reorder_choices_out_of_bounds() {
    let choices = vec!["A".to_string(), "B".to_string()];
    assert!(reorder_choices(&choices, 2).is_err());
}

#[test]
fn test_reorder_choices_min_max_bounds() {
    let single = vec!["Solo".to_string()];
    assert!(reorder_choices(&single, 0).is_err());

    let too_many = vec![
        "1".to_string(),
        "2".to_string(),
        "3".to_string(),
        "4".to_string(),
        "5".to_string(),
        "6".to_string(),
        "7".to_string(),
    ];
    assert!(reorder_choices(&too_many, 0).is_err());
}

#[test]
fn test_validate_exhausted_fallback_mismatched_answer_rejected() {
    let source = vec!["A".to_string(), "B".to_string()];
    let fallback = vec!["B".to_string(), "A".to_string()];
    // Source answer is 0 ("A"), fallback answer claims 0 ("B") -> mismatch!
    assert!(validate_exhausted_fallback_variant(&source, 0, &fallback, 0).is_err());
    // Correct fallback answer is 1 ("A") -> ok
    assert!(validate_exhausted_fallback_variant(&source, 0, &fallback, 1).is_ok());
}
