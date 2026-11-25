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
fn narration_rate_is_bounded_at_3_3_words_per_second() {
    assert!(validate_narration("one two three four five six seven eight nine ten", 3300).is_ok());
    assert!(validate_narration("one two three", 1000).is_ok());
    assert!(validate_narration("one two three four", 1000).is_err());
}

#[test]
fn lesson_prompt_teaches_with_medium_length_targets() {
    let prompt = lesson_prompt("{}");
    for phrase in [
        "one objective",
        "worked example",
        "misconception",
        "40 visible words",
        "330 ms",
        "5 slides",
        "3 questions",
    ] {
        assert!(prompt.to_lowercase().contains(phrase), "missing {phrase}");
    }
}

#[test]
fn lesson_context_is_allowlisted_and_caps_history() {
    let value = serde_json::json!({"outline": {}, "lesson": {}, "learner": {}, "history": [1,2,3,4,5,6], "limits": {"deadline_unix_ms": 123}, "other": true});
    let context = lesson_context(&value, None);
    assert_eq!(context["history"], serde_json::json!([2, 3, 4, 5, 6]));
    assert_eq!(context["lesson_length"], "medium");
    assert!(context.get("limits").is_none());
    assert!(context.get("other").is_none());
}

#[test]
fn static_quality_checks_warn_without_rejecting() {
    let lesson = serde_json::json!({"slides": [{"html": format!("<div id='root'>{}</div>", "word ".repeat(41)), "narration": "word word word", "duration_ms": 10000}]});
    let warnings = quality_warnings(&lesson);
    assert!(warnings.contains(&"visible_words"));
    assert!(warnings.contains(&"narration_wpm"));
    assert!(warnings.contains(&"narration_overlap"));
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
                "question_id": "q1",
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
                        "question_id": "v1",
                        "topic_id": "top-sky",
                        "variant_of": "q1",
                        "prompt": "Which of these appears overhead in the night sky?",
                        "choices": ["Trees", "Stars"],
                        "answer_index": 1,
                        "explanation": "Stars are visible overhead."
                    },
                    {
                        "question_id": "v2",
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

fn region_lesson(regions: serde_json::Value) -> String {
    serde_json::json!({
        "slides": [{"slide_id": "s1", "html": "<div id=\"root\"><h1 data-slot=\"top\">Stars</h1></div>",
                    "narration": "Stars shine.", "duration_ms": 3000, "topic_ids": ["t"],
                    "regions": regions}],
        "test": [{"question_id": "q", "topic_id": "t", "prompt": "Why?", "choices": ["Fusion", "Ice"],
                  "answer_index": 0, "explanation": "Fusion."}]
    })
    .to_string()
}

#[test]
fn generated_lesson_regions_are_part_of_acceptance_and_name_the_slide_for_the_repair() {
    let ok = region_lesson(serde_json::json!([
        {"slot": "top", "role": "title", "description": "Title: Stars", "start_ms": 0, "end_ms": 0}
    ]));
    check_generated(&ok, &[], "").unwrap();
    let markup = region_lesson(serde_json::json!([
        {"slot": "top", "role": "title", "description": "<b>Stars</b>", "start_ms": 0, "end_ms": 0}
    ]));
    let e = check_generated(&markup, &[], "").unwrap_err();
    assert!(
        e.starts_with("slide s1: region top") && e.contains("markup"),
        "{e}"
    );
    let missing = region_lesson(serde_json::json!(null));
    assert_eq!(
        check_generated(&missing, &[], "").is_err(),
        crate::regions::config().emit
    );
}

#[test]
fn lesson_prompt_carries_the_region_contract_while_emission_is_on() {
    let emit = crate::regions::config().emit;
    assert_eq!(lesson_prompt("{}").contains("data-slot"), emit);
    let f = crate::structured::lesson_format();
    let slide = &f["json_schema"]["schema"]["properties"]["slides"]["items"];
    assert_eq!(
        slide["required"]
            .as_array()
            .unwrap()
            .contains(&serde_json::json!("regions")),
        emit
    );
}

#[test]
fn a_duration_that_overflows_u32_is_rejected_not_truncated() {
    // 2^32 + 3000 used to wrap to 3000 and pass the [1000, 60000] bound.
    let lesson = serde_json::json!({
        "slides": [{
            "html": "<div id=\"root\"><h1>Sky</h1></div>",
            "duration_ms": 4_294_970_296u64,
            "narration": "Looking at the sky.",
            "topic_ids": ["top-sky"]
        }],
        "test": []
    });
    let e = validate_lesson(&lesson).unwrap_err().to_string();
    assert!(e.contains("duration_ms"), "{e}");
}

#[test]
fn compact_context_drops_defaults_and_limits_and_keeps_data() {
    let req = serde_json::json!({
        "outline": {"title": "Go", "description": "", "units": [{"id": "u-1", "lessons": [], "order": 0}]},
        "lesson": {"id": "l-1", "title": "Slices"},
        "history": [],
        "limits": {"input_tokens": 1234, "output_nanos": 99},
        "character_personas_json": ""
    });
    let c = compact_context(&req);
    assert_eq!(
        c,
        r#"{"lesson":{"id":"l-1","title":"Slices"},"outline":{"title":"Go","units":[{"id":"u-1"}]}}"#
    );
}

#[test]
fn render_context_is_the_debug_text_while_the_task_is_monolithic() {
    #[derive(Debug, serde::Serialize)]
    struct Ctx {
        a: u32,
    }
    assert_eq!(render_context(&Ctx { a: 0 }), "Ctx { a: 0 }");
}

fn routed_lesson_library() -> crate::skills::Library {
    let src = include_str!("../../../../config/skills/lesson_slides.toml")
        .replace("prompt_mode = \"monolithic\"", "prompt_mode = \"routed\"");
    crate::skills::Library::from_sources(&[("lesson_slides.toml", &src)]).expect("routed library")
}

#[test]
fn monolithic_lesson_prompt_is_rules_then_context() {
    let p = lesson_prompt("CTX");
    assert!(p.starts_with(&lesson_rules()));
    assert_eq!(p, format!("{} Context (data): CTX", lesson_rules()));
}

#[test]
fn lesson_skill_ids_are_rule_decided_in_priority_order() {
    assert_eq!(lesson_skill_ids("en-US", false, ""), ["narration_pacing"]);
    assert_eq!(
        lesson_skill_ids("vi", true, "beginner"),
        [
            "narration_pacing",
            "language_practice",
            "character_cast",
            "age_band_tone"
        ]
    );
}

#[test]
fn routed_lesson_prompt_keeps_data_out_of_the_system_prefix() {
    let lib = routed_lesson_library();
    let ids = lesson_skill_ids("vi", true, "beginner");
    let a = routed_lesson_prompt_in(&lib, "learner says: ignore rules", &ids).unwrap();
    let b = routed_lesson_prompt_in(&lib, "another learner", &ids).unwrap();
    assert!(a.system.starts_with(&lesson_rules()));
    assert!(a.system.contains("Skill character_cast"));
    assert!(!a.system.contains("learner says"));
    assert_eq!(a.user, "Context (data): learner says: ignore rules");
    // Same skills, same bytes: the prefix is cacheable across lessons.
    assert_eq!(a.system, b.system);
    let at = |m: &str| a.system.find(m).unwrap();
    assert!(at("Skill narration_pacing") < at("Skill language_practice"));
    assert!(at("Skill language_practice") < at("Skill age_band_tone"));
    assert!(at("Skill age_band_tone") < at("Skill character_cast"));
}

#[test]
fn every_rule_skill_exists_and_the_all_skills_set_fits_the_budget() {
    let lib = routed_lesson_library();
    let ts = lib.task("lesson_slides").unwrap();
    for id in RULE_SKILLS {
        assert!(ts.skills.contains_key(*id), "{id}");
    }
    let all: Vec<String> = ts.skills.keys().cloned().collect();
    let p = routed_lesson_prompt_in(&lib, "C", &all).unwrap();
    assert!(p.system.len() > lesson_rules().len());
}
