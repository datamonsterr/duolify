use super::*;

fn slide(html: &str, regions: Value) -> Value {
    json!({"slide_id":"s1","html":html,"duration_ms":8000,"narration":"n","topic_ids":["t"],"regions":regions})
}

fn region(slot: &str, role: &str, description: &str) -> Value {
    json!({"slot":slot,"role":role,"description":description,"start_ms":0,"end_ms":0})
}

fn timed(slot: &str, start: u64, end: u64) -> Value {
    json!({"slot":slot,"role":"example","description":"x","start_ms":start,"end_ms":end})
}

const HTML: &str = r#"<div id="root" style="background:#fff"><h1 data-slot="top">Slices are views</h1><svg data-slot="left" viewBox="0 0 10 10"><rect x="1" y="1" width="8" height="8"/><text x="2" y="5">array</text></svg><pre data-slot="right">s := a[1:3]</pre></div>"#;

fn valid() -> Value {
    slide(
        HTML,
        json!([
            region("top", "title", "Title: slices are views over an array"),
            region("left", "diagram", "Diagram of the backing array"),
            region("right", "code", "Code slicing a with s := a[1:3]"),
        ]),
    )
}

#[test]
fn config_has_a_box_inside_the_stage_for_every_slot() {
    let cfg = config();
    assert!(cfg.max_per_slide >= 5 && cfg.max_description_chars > 0);
    for (name, [x, y, w, h]) in &cfg.slots {
        assert!(
            *x >= 0.0 && *y >= 0.0 && *w > 0.0 && *h > 0.0 && x + w <= 1.0 && y + h <= 1.0,
            "{name}"
        );
    }
}

#[test]
fn valid_regions_take_the_box_and_name_of_their_slot() {
    let regions = slide_regions(&valid()).unwrap();
    assert_eq!(regions.len(), 3);
    assert_eq!(regions[0].bbox, config().slots["top"]);
    assert_eq!(regions[0].id, "top");
    assert_eq!(regions[2].role, "code");
    assert_eq!(regions[1].window, None);
}

#[test]
fn five_regions_on_one_slide_are_valid() {
    let html = r#"<div id="root"><h1 data-slot="top">A</h1><p data-slot="left">B</p><p data-slot="right_top">C</p><p data-slot="right_bottom">D</p><p data-slot="bottom">E</p></div>"#;
    let s = slide(
        html,
        json!([
            region("top", "title", "A"),
            region("left", "explanation", "B"),
            region("right_top", "example", "C"),
            region("right_bottom", "question", "D"),
            region("bottom", "caption", "E"),
        ]),
    );
    assert_eq!(slide_regions(&s).unwrap().len(), 5);
}

#[test]
fn unknown_slot_role_and_reused_slots_are_rejected() {
    let html = r#"<div id="root"><p data-slot="top">A</p></div>"#;
    for (regions, needle) in [
        (json!([region("middle", "title", "A")]), "unknown"),
        (json!([region("top", "headline", "A")]), "unknown role"),
        (
            json!([region("top", "title", "A"), region("top", "caption", "A")]),
            "more than one region",
        ),
    ] {
        let e = slide_regions(&slide(html, regions)).unwrap_err();
        assert!(e.contains(needle), "{e}");
    }
}

#[test]
fn count_and_length_limits_are_enforced() {
    let cfg = config();
    let html = r#"<div id="root"><p data-slot="top">A</p></div>"#;
    assert!(slide_regions(&slide(html, json!([]))).is_err() || !cfg.emit);
    let many: Vec<Value> = (0..=cfg.max_per_slide)
        .map(|_| region("top", "title", "x"))
        .collect();
    assert!(
        slide_regions(&slide(html, json!(many)))
            .unwrap_err()
            .contains("regions must list")
    );
    let long = "a".repeat(cfg.max_description_chars + 1);
    assert!(
        slide_regions(&slide(html, json!([region("top", "title", &long)])))
            .unwrap_err()
            .contains("characters")
    );
    let exact = "a".repeat(cfg.max_description_chars);
    assert!(slide_regions(&slide(html, json!([region("top", "title", &exact)]))).is_ok());
}

#[test]
fn markup_entities_and_control_characters_are_rejected() {
    let html = r#"<div id="root"><p data-slot="top">A</p></div>"#;
    for bad in [
        "<b>bold</b>",
        "see <img src=x>",
        "a &amp; b",
        "tab\there",
        "line\nbreak",
        "rtl \u{202E}override",
        "<!-- hidden -->",
    ] {
        assert!(
            slide_regions(&slide(html, json!([region("top", "title", bad)]))).is_err(),
            "{bad:?}"
        );
    }
    // Comparisons and code stay plain text.
    for ok in [
        "i < n and n > 0",
        "s := a[1:3]",
        "AT&T",
        "Yo soy estudiante",
    ] {
        assert!(
            slide_regions(&slide(html, json!([region("top", "code", ok)]))).is_ok(),
            "{ok:?}"
        );
    }
}

#[test]
fn every_region_needs_one_element_inside_the_root_and_outside_other_slots() {
    let r = |html: &str| slide_regions(&slide(html, json!([region("top", "title", "A")])));
    for (html, why) in [
        (r#"<div id="root"><p>A</p></div>"#, "no element"),
        (
            r#"<div id="root"><p data-slot="top">A</p><p data-slot="top">B</p></div>"#,
            "more than one element",
        ),
        (
            r#"<p data-slot="top">A</p><div id="root"></div>"#,
            "outside the root",
        ),
    ] {
        let e = r(html).unwrap_err();
        assert!(e.contains(why), "{html}: {e}");
    }
    let nested = r#"<div id="root"><div data-slot="bottom"><p data-slot="top">A</p></div></div>"#;
    let two = slide(
        nested,
        json!([
            region("bottom", "caption", "A"),
            region("top", "title", "A")
        ]),
    );
    assert!(
        slide_regions(&two)
            .unwrap_err()
            .contains("inside another slot")
    );
    // Wrappers, void and self-closing elements are fine: the slot stylesheet neutralizes the
    // containing-block properties of every ancestor.
    assert!(r(r##"<div id='root'><main class="card"><img src="#x"><br/><section><h1 data-slot='top'>A</h1></section></main></div>"##).is_ok());
}

#[test]
fn important_declarations_are_rejected_because_they_could_move_a_region() {
    let html = r#"<div id="root"><p data-slot="top" style="left:0 !IMPORTANT">A</p></div>"#;
    assert!(
        slide_regions(&slide(html, json!([region("top", "title", "A")])))
            .unwrap_err()
            .contains("!important")
    );
}

#[test]
fn time_windows_stay_on_the_slide_clock() {
    let html = r#"<div id="root"><p data-slot="top">A</p></div>"#;
    let with = |s: u64, e: u64| slide(html, json!([timed("top", s, e)]));
    assert_eq!(
        slide_regions(&with(1000, 8000)).unwrap()[0].window,
        Some((1000, 8000))
    );
    for (s, e) in [(5000, 5000), (6000, 2000), (0, 8001)] {
        assert!(slide_regions(&with(s, e)).is_err(), "{s}-{e}");
    }
}

#[test]
fn overlapping_slots_are_rejected_only_when_both_are_visible() {
    let html = r#"<div id="root"><p data-slot="body">A</p><p data-slot="left">B</p></div>"#;
    let pair = |w1: (u64, u64), w2: (u64, u64)| {
        slide(
            html,
            json!([timed("body", w1.0, w1.1), timed("left", w2.0, w2.1)]),
        )
    };
    assert!(
        slide_regions(&pair((0, 0), (0, 0)))
            .unwrap_err()
            .contains("overlap")
    );
    assert!(slide_regions(&pair((0, 4000), (2000, 8000))).is_err());
    assert!(slide_regions(&pair((0, 4000), (4000, 8000))).is_ok());
}

#[test]
fn box_overlap_is_intersection_over_the_smaller_box() {
    let a = [0.0, 0.0, 0.5, 0.5];
    assert_eq!(overlap(a, a), 1.0);
    assert_eq!(overlap(a, [0.5, 0.0, 0.5, 0.5]), 0.0);
    assert!((overlap(a, [0.25, 0.0, 0.5, 0.5]) - 0.5).abs() < 1e-9);
    assert_eq!(overlap(a, [0.1, 0.1, 0.1, 0.1]), 1.0);
    let cfg = config();
    // The designed grid never overlaps between band, halves and quarters of one column.
    for (x, y) in [
        ("top", "body"),
        ("body", "bottom"),
        ("left", "right"),
        ("left_top", "left_bottom"),
        ("left_top", "right_top"),
    ] {
        assert_eq!(overlap(cfg.slots[x], cfg.slots[y]), 0.0, "{x} {y}");
    }
}

#[test]
fn layout_pins_each_region_element_to_its_slot_and_passes_the_static_check() {
    let regions = slide_regions(&valid()).unwrap();
    let html = layout(HTML, &regions);
    assert!(html.ends_with(HTML));
    assert!(html.starts_with("<style data-duolify-regions>"));
    assert!(
        html.contains(r#"#root#root [data-slot="top"]{position:fixed!important;left:5%!important;top:4%!important;width:90%!important;height:18%!important"#),
        "{html}"
    );
    crate::slide::validate(&html, &[]).unwrap();
    assert_eq!(layout(HTML, &[]), HTML);
}

#[test]
fn element_text_and_faithfulness_follow_the_html() {
    let found = elements(HTML);
    assert_eq!(found["top"].text, "Slices are views");
    assert_eq!(found["left"].text, "array");
    let regions = slide_regions(&valid()).unwrap();
    assert_eq!(faithfulness(HTML, &regions), 1.0);
    let mut wrong = regions.clone();
    wrong[0].description = "Photosynthesis in green leaves".into();
    assert!((faithfulness(HTML, &wrong) - 2.0 / 3.0).abs() < 1e-9);
}

#[test]
fn emission_off_accepts_slides_without_regions_but_still_checks_declared_ones() {
    let mut cfg = config().clone();
    cfg.emit = false;
    let html = r#"<div id="root"><p>A</p></div>"#;
    assert!(
        slide_regions_with(&cfg, &json!({"html":html,"duration_ms":3000}))
            .unwrap()
            .is_empty()
    );
    assert!(slide_regions_with(&cfg, &slide(html, json!([region("top", "title", "A")]))).is_err());
    cfg.emit = true;
    assert!(slide_regions_with(&cfg, &json!({"html":html,"duration_ms":3000})).is_err());
}

#[test]
fn prompt_and_schema_name_every_slot_and_role() {
    let shape = shape();
    let schema = schema();
    for slot in config().slots.keys() {
        assert!(!config().emit || shape.contains(slot.as_str()), "{slot}");
        assert!(
            schema["items"]["properties"]["slot"]["enum"]
                .as_array()
                .unwrap()
                .contains(&json!(slot))
        );
    }
    for role in ROLES {
        assert!(!config().emit || shape.contains(role));
    }
}

#[test]
fn a_window_that_overflows_u32_is_rejected_not_truncated() {
    let html = r#"<div id="root"><p data-slot="top">A</p></div>"#;
    let big = (1u64 << 32) + 3000;
    let mut s = slide(html, json!([timed("top", 2000, big)]));
    s["duration_ms"] = json!(big);
    let e = slide_regions(&s).unwrap_err();
    assert!(e.contains("start_ms and end_ms"), "{e}");
}
