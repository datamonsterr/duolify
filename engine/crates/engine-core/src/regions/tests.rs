use super::*;

fn slide(html: &str, regions: Value) -> Value {
    json!({"slide_id":"s1","html":html,"duration_ms":8000,"narration":"n","topic_ids":["t"],"regions":regions})
}

fn region(id: &str, slot: &str, role: &str, description: &str) -> Value {
    json!({"id":id,"slot":slot,"role":role,"description":description,"start_ms":0,"end_ms":0})
}

const HTML: &str = r#"<div id="root" style="background:#fff"><h1 data-region="t">Slices are views</h1><svg data-region="d" viewBox="0 0 10 10"><rect x="1" y="1" width="8" height="8"/><text x="2" y="5">array</text></svg><pre data-region="c">s := a[1:3]</pre></div>"#;

fn valid() -> Value {
    slide(
        HTML,
        json!([
            region("t", "top", "title", "Title: slices are views over an array"),
            region("d", "left", "diagram", "Diagram of the backing array"),
            region("c", "right", "code", "Code slicing a with s := a[1:3]"),
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
fn valid_regions_take_the_box_of_their_slot() {
    let regions = slide_regions(&valid()).unwrap();
    assert_eq!(regions.len(), 3);
    assert_eq!(regions[0].bbox, config().slots["top"]);
    assert_eq!(regions[2].role, "code");
    assert_eq!(regions[1].window, None);
}

#[test]
fn five_regions_on_one_slide_are_valid() {
    let html = r#"<div id="root"><h1 data-region="a">A</h1><p data-region="b">B</p><p data-region="c">C</p><p data-region="d">D</p><p data-region="e">E</p></div>"#;
    let s = slide(
        html,
        json!([
            region("a", "top", "title", "A"),
            region("b", "left", "explanation", "B"),
            region("c", "right_top", "example", "C"),
            region("d", "right_bottom", "question", "D"),
            region("e", "bottom", "caption", "E"),
        ]),
    );
    assert_eq!(slide_regions(&s).unwrap().len(), 5);
}

#[test]
fn unknown_slot_role_and_duplicate_ids_are_rejected() {
    let html = r#"<div id="root"><p data-region="a">A</p></div>"#;
    for (regions, needle) in [
        (json!([region("a", "middle", "title", "A")]), "unknown slot"),
        (json!([region("a", "top", "headline", "A")]), "unknown role"),
        (
            json!([
                region("a", "top", "title", "A"),
                region("a", "bottom", "caption", "A")
            ]),
            "unique",
        ),
        (json!([region("a b", "top", "title", "A")]), "letters"),
    ] {
        let e = slide_regions(&slide(html, regions)).unwrap_err();
        assert!(e.contains(needle), "{e}");
    }
}

#[test]
fn count_and_length_limits_are_enforced() {
    let cfg = config();
    let html = r#"<div id="root"><p data-region="a">A</p></div>"#;
    assert!(slide_regions(&slide(html, json!([]))).is_err() || !cfg.emit);
    let many: Vec<Value> = (0..=cfg.max_per_slide)
        .map(|i| region(&format!("r{i}"), "top", "title", "x"))
        .collect();
    assert!(
        slide_regions(&slide(html, json!(many)))
            .unwrap_err()
            .contains("regions must list")
    );
    let long = "a".repeat(cfg.max_description_chars + 1);
    assert!(
        slide_regions(&slide(html, json!([region("a", "top", "title", &long)])))
            .unwrap_err()
            .contains("characters")
    );
    let exact = "a".repeat(cfg.max_description_chars);
    assert!(slide_regions(&slide(html, json!([region("a", "top", "title", &exact)]))).is_ok());
}

#[test]
fn markup_entities_and_control_characters_are_rejected() {
    let html = r#"<div id="root"><p data-region="a">A</p></div>"#;
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
            slide_regions(&slide(html, json!([region("a", "top", "title", bad)]))).is_err(),
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
            slide_regions(&slide(html, json!([region("a", "top", "code", ok)]))).is_ok(),
            "{ok:?}"
        );
    }
}

#[test]
fn every_region_needs_one_element_inside_the_root_and_outside_other_regions() {
    let r = |html: &str| slide_regions(&slide(html, json!([region("a", "top", "title", "A")])));
    for (html, why) in [
        (r#"<div id="root"><p>A</p></div>"#, "no element"),
        (
            r#"<div id="root"><p data-region="a">A</p><p data-region="a">B</p></div>"#,
            "more than one",
        ),
        (
            r#"<p data-region="a">A</p><div id="root"></div>"#,
            "outside the root",
        ),
    ] {
        let e = r(html).unwrap_err();
        assert!(e.contains(why), "{html}: {e}");
    }
    let nested = r#"<div id="root"><div data-region="b"><p data-region="a">A</p></div></div>"#;
    let two = slide(
        nested,
        json!([
            region("b", "bottom", "caption", "A"),
            region("a", "top", "title", "A")
        ]),
    );
    assert!(
        slide_regions(&two)
            .unwrap_err()
            .contains("inside another region")
    );
    // Wrappers are fine: the slot stylesheet neutralizes their containing-block properties.
    assert!(r(r#"<div id="root"><main class="card"><section><h1 data-region="a">A</h1></section></main></div>"#).is_ok());
    // Void and self-closing siblings do not change the depth.
    let ok = r##"<div id='root'><img src="#x"><br/><p data-region='a'>A</p></div>"##;
    assert!(slide_regions(&slide(ok, json!([region("a", "top", "title", "A")]))).is_ok());
}

#[test]
fn important_declarations_are_rejected_because_they_could_move_a_region() {
    let html = r#"<div id="root"><p data-region="a" style="left:0 !IMPORTANT">A</p></div>"#;
    assert!(
        slide_regions(&slide(html, json!([region("a", "top", "title", "A")])))
            .unwrap_err()
            .contains("!important")
    );
}

#[test]
fn time_windows_stay_on_the_slide_clock() {
    let html = r#"<div id="root"><p data-region="a">A</p></div>"#;
    let with = |s: u64, e: u64| {
        slide(
            html,
            json!([{"id":"a","slot":"top","role":"title","description":"A","start_ms":s,"end_ms":e}]),
        )
    };
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
    let html = r#"<div id="root"><p data-region="a">A</p><p data-region="b">B</p></div>"#;
    let pair = |w1: (u64, u64), w2: (u64, u64)| {
        slide(
            html,
            json!([
                {"id":"a","slot":"body","role":"explanation","description":"A","start_ms":w1.0,"end_ms":w1.1},
                {"id":"b","slot":"left","role":"example","description":"B","start_ms":w2.0,"end_ms":w2.1}
            ]),
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
    assert!(html.starts_with(HTML));
    assert!(html.contains(r#"#root#root [data-region="t"]{position:fixed!important;left:5%!important;top:4%!important;width:90%!important;height:18%!important"#), "{html}");
    crate::slide::validate(&html, &[]).unwrap();
    assert_eq!(layout(HTML, &[]), HTML);
}

#[test]
fn element_text_and_faithfulness_follow_the_html() {
    let found = elements(HTML);
    assert_eq!(found["t"].text, "Slices are views");
    assert_eq!(found["d"].text, "array");
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
    assert!(
        slide_regions_with(
            &cfg,
            &slide(html, json!([region("a", "top", "title", "A")]))
        )
        .is_err()
    );
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
