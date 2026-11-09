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
                "narration": "Looking at the sky."
            }
        ],
        "test": [
            {
                "prompt": "What is in the sky?",
                "choices": ["Stars", "Trees"],
                "answer_index": 0,
                "explanation": "Stars are in the sky."
            }
        ]
    });
    assert!(validate_lesson(&valid_json).is_ok());
}
