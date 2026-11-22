//! Slide regions (openspec change immersive-mobile-lesson-player, design decision 6).
//!
//! A generated Slide names a layout slot for each region instead of guessing coordinates. The
//! Engine owns every slot's box (`engine/config/regions.toml`) and appends a stylesheet that pins
//! each `data-region` element, a direct child of the root element, to its slot. The declared box is
//! therefore the box the element occupies on the 16:9 stage, by construction.
use regex::Regex;
use serde::Deserialize;
use serde_json::{Value, json};
use std::collections::{BTreeMap, HashMap, HashSet};
use std::sync::OnceLock;

/// The closed role set of the Slide manifest (`proto` `RegionRole`, API `player.RegionRoles`).
pub const ROLES: [&str; 7] = [
    "title",
    "explanation",
    "example",
    "diagram",
    "code",
    "question",
    "caption",
];
/// Longest region id; ids use letters, digits, `-` and `_`.
pub const MAX_ID_CHARS: usize = 40;

#[derive(Debug, Clone, Deserialize)]
pub struct Config {
    pub emit: bool,
    pub max_per_slide: usize,
    pub max_description_chars: usize,
    pub max_overlap: f64,
    pub slots: BTreeMap<String, [f64; 4]>,
}

/// The embedded `engine/config/regions.toml` (fingerprinted by `promotion::SOURCES`).
pub fn config() -> &'static Config {
    static CONFIG: OnceLock<Config> = OnceLock::new();
    CONFIG.get_or_init(|| {
        toml::from_str(include_str!("../../../config/regions.toml"))
            .expect("engine/config/regions.toml is valid")
    })
}

/// One validated region with the box of its slot.
#[derive(Debug, Clone, PartialEq)]
pub struct Region {
    pub id: String,
    pub slot: String,
    pub role: String,
    pub description: String,
    /// Slide-clock window in ms; `None` is the whole Slide.
    pub window: Option<(u32, u32)>,
    /// Normalized `[x, y, w, h]` of the stage.
    pub bbox: [f64; 4],
}

/// Why `text` is not plain text: control or bidi override characters, a tag or an HTML entity.
pub fn plain_text_violation(text: &str) -> Option<&'static str> {
    if text
        .chars()
        .any(|c| c.is_control() || matches!(c, '\u{202A}'..='\u{202E}' | '\u{2066}'..='\u{2069}'))
    {
        return Some("control characters");
    }
    static MARKUP: OnceLock<Regex> = OnceLock::new();
    let markup = MARKUP.get_or_init(|| {
        Regex::new(r"(?i)<[a-z/!?]|&(?:#[0-9]+|#x[0-9a-f]+|[a-z][a-z0-9]*);").unwrap()
    });
    markup.is_match(text).then_some("markup")
}

/// Intersection area over the smaller box's area (0 when either box is empty).
pub fn overlap(a: [f64; 4], b: [f64; 4]) -> f64 {
    let w = (a[0] + a[2]).min(b[0] + b[2]) - a[0].max(b[0]);
    let h = (a[1] + a[3]).min(b[1] + b[3]) - a[1].max(b[1]);
    let smaller = (a[2] * a[3]).min(b[2] * b[3]);
    if w <= 0.0 || h <= 0.0 || smaller <= 0.0 {
        return 0.0;
    }
    w * h / smaller
}

fn windows_meet(a: Option<(u32, u32)>, b: Option<(u32, u32)>) -> bool {
    match (a, b) {
        (Some((s1, e1)), Some((s2, e2))) => s1 < e2 && s2 < e1,
        _ => true,
    }
}

/// What the html places under one `data-region` value.
#[derive(Debug, Default, Clone)]
pub struct Element {
    pub count: usize,
    /// Every occurrence is a direct child of the root element.
    pub direct: bool,
    /// Visible text inside the element (whitespace collapsed).
    pub text: String,
}

/// Scans tags (comments, scripts and styles removed) and returns each `data-region` element.
pub fn elements(html: &str) -> HashMap<String, Element> {
    static RE: OnceLock<[Regex; 6]> = OnceLock::new();
    let [comment, script, style, tag, region, root] = RE.get_or_init(|| {
        [
            Regex::new(r"(?s)<!--.*?-->").unwrap(),
            Regex::new(r"(?is)<script\b[^>]*>.*?</script\s*>").unwrap(),
            Regex::new(r"(?is)<style\b[^>]*>.*?</style\s*>").unwrap(),
            Regex::new(r#"<(/?)([A-Za-z][A-Za-z0-9:-]*)((?:[^>"']|"[^"]*"|'[^']*')*)>"#).unwrap(),
            Regex::new(r#"(?i)(?:^|\s)data-region\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>/]+))"#)
                .unwrap(),
            Regex::new(r#"(?i)(?:^|\s)id\s*=\s*(?:"root"|'root'|root(?:\s|/|$))"#).unwrap(),
        ]
    });
    const VOID: [&str; 14] = [
        "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source",
        "track", "wbr", "param",
    ];
    let html = comment.replace_all(html, "");
    let html = script.replace_all(&html, "");
    let html = style.replace_all(&html, "");
    // (tag name, region id, is root)
    let mut stack: Vec<(String, Option<String>, bool)> = Vec::new();
    let mut found: HashMap<String, Element> = HashMap::new();
    let mut last = 0;
    fn add_text(
        found: &mut HashMap<String, Element>,
        stack: &[(String, Option<String>, bool)],
        text: &str,
    ) {
        let words = text.split_whitespace().collect::<Vec<_>>().join(" ");
        if words.is_empty() {
            return;
        }
        for id in stack.iter().filter_map(|(_, r, _)| r.as_ref()) {
            let e = found.entry(id.clone()).or_default();
            if !e.text.is_empty() {
                e.text.push(' ');
            }
            e.text.push_str(&words);
        }
    }
    let mut opened: Vec<(String, bool)> = Vec::new();
    for m in tag.captures_iter(&html) {
        let whole = m.get(0).unwrap();
        add_text(&mut found, &stack, &html[last..whole.start()]);
        last = whole.end();
        let name = m[2].to_ascii_lowercase();
        let attrs = &m[3];
        if &m[1] == "/" {
            if let Some(at) = stack.iter().rposition(|(t, _, _)| *t == name) {
                stack.truncate(at);
            }
            continue;
        }
        let id = region.captures(attrs).and_then(|c| {
            c.get(1)
                .or_else(|| c.get(2))
                .or_else(|| c.get(3))
                .map(|v| v.as_str().to_owned())
        });
        if let Some(id) = &id {
            let direct = stack.last().is_some_and(|(_, _, r)| *r);
            opened.push((id.clone(), direct));
            found.entry(id.clone()).or_default();
        }
        let self_closing = attrs.trim_end().ends_with('/') || VOID.contains(&name.as_str());
        if !self_closing {
            stack.push((name, id, root.is_match(attrs)));
        }
    }
    add_text(&mut found, &stack, &html[last..]);
    for (id, direct) in opened {
        let e = found.get_mut(&id).expect("recorded");
        e.direct = if e.count == 0 {
            direct
        } else {
            e.direct && direct
        };
        e.count += 1;
    }
    found
}

/// Regions of one generated Slide JSON object, validated against its html and duration.
/// A Slide without a `regions` key yields none unless the configuration requires them.
pub fn slide_regions(slide: &Value) -> Result<Vec<Region>, String> {
    slide_regions_with(config(), slide)
}

pub fn slide_regions_with(cfg: &Config, slide: &Value) -> Result<Vec<Region>, String> {
    let empty = Vec::new();
    let items = match &slide["regions"] {
        Value::Null => &empty,
        Value::Array(items) => items,
        _ => return Err("regions must be an array".into()),
    };
    if items.is_empty() && !cfg.emit {
        return Ok(Vec::new());
    }
    if items.is_empty() || items.len() > cfg.max_per_slide {
        return Err(format!(
            "regions must list 1 to {} regions, one per visible block",
            cfg.max_per_slide
        ));
    }
    let html = slide["html"].as_str().unwrap_or_default();
    if html.to_ascii_lowercase().contains("!important") {
        return Err(
            "slide html must not use !important (the player places region elements in their slots)"
                .into(),
        );
    }
    let duration = slide["duration_ms"].as_u64().unwrap_or(0);
    let found = elements(html);
    let mut seen = HashSet::new();
    let mut out: Vec<Region> = Vec::with_capacity(items.len());
    for item in items {
        let id = item["id"].as_str().unwrap_or_default();
        if id.is_empty()
            || id.chars().count() > MAX_ID_CHARS
            || !id
                .chars()
                .all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '_')
            || !seen.insert(id)
        {
            return Err(format!(
                "region id {id:?} must be 1 to {MAX_ID_CHARS} letters, digits, - or _ and unique in the slide"
            ));
        }
        let slot = item["slot"].as_str().unwrap_or_default();
        let Some(bbox) = cfg.slots.get(slot) else {
            let names: Vec<&str> = cfg.slots.keys().map(String::as_str).collect();
            return Err(format!(
                "region {id}: unknown slot {slot:?}; use one of {}",
                names.join(", ")
            ));
        };
        let role = item["role"].as_str().unwrap_or_default();
        if !ROLES.contains(&role) {
            return Err(format!(
                "region {id}: unknown role {role:?}; use one of {}",
                ROLES.join(", ")
            ));
        }
        let description = item["description"].as_str().unwrap_or_default();
        let chars = description.trim().chars().count();
        if chars == 0 || description.chars().count() > cfg.max_description_chars {
            return Err(format!(
                "region {id}: description must be 1 to {} characters",
                cfg.max_description_chars
            ));
        }
        if let Some(why) = plain_text_violation(description) {
            return Err(format!(
                "region {id}: description must be plain text without {why}"
            ));
        }
        let start = item["start_ms"].as_u64().unwrap_or(0);
        let end = item["end_ms"].as_u64().unwrap_or(0);
        let window = if start == 0 && end == 0 {
            None
        } else if start < end && end <= duration {
            Some((start as u32, end as u32))
        } else {
            return Err(format!(
                "region {id}: start_ms and end_ms must be 0 and 0 (whole slide) or 0 <= start_ms < end_ms <= duration_ms ({duration})"
            ));
        };
        match found.get(id) {
            Some(e) if e.count == 1 && e.direct => {}
            _ => {
                return Err(format!(
                    "region {id}: the html needs exactly one element with data-region=\"{id}\" as a direct child of the root element"
                ));
            }
        }
        let region = Region {
            id: id.to_owned(),
            slot: slot.to_owned(),
            role: role.to_owned(),
            description: description.trim().to_owned(),
            window,
            bbox: *bbox,
        };
        if let Some(other) = out.iter().find(|o| {
            windows_meet(o.window, region.window) && overlap(o.bbox, region.bbox) > cfg.max_overlap
        }) {
            return Err(format!(
                "regions {} ({}) and {id} ({slot}) overlap while both are visible; use slots that do not overlap or disjoint start_ms/end_ms",
                other.id, other.slot
            ));
        }
        out.push(region);
    }
    Ok(out)
}

/// Validates the regions of every Slide of a generated Lesson, naming the Slide for the repair.
pub fn validate_lesson(lesson: &Value) -> Result<(), String> {
    for slide in lesson["slides"].as_array().into_iter().flatten() {
        slide_regions(slide).map_err(|e| {
            format!(
                "slide {}: {e}",
                slide["slide_id"].as_str().unwrap_or_default()
            )
        })?;
    }
    Ok(())
}

fn pct(v: f64) -> String {
    let s = format!("{:.2}", v * 100.0);
    let s = s.trim_end_matches('0').trim_end_matches('.');
    format!("{s}%")
}

/// The Slide html with the Engine's slot stylesheet appended (unchanged without regions).
pub fn layout(html: &str, regions: &[Region]) -> String {
    if regions.is_empty() {
        return html.to_owned();
    }
    let mut css = String::from(
        "#root#root{position:fixed!important;left:0!important;top:0!important;width:100%!important;height:100%!important;margin:0!important;border:0!important;box-sizing:border-box!important}",
    );
    for r in regions {
        let [x, y, w, h] = r.bbox;
        css.push_str(&format!(
            "#root#root>[data-region=\"{}\"]{{position:absolute!important;left:{}!important;top:{}!important;width:{}!important;height:{}!important;margin:0!important;box-sizing:border-box!important;overflow:hidden!important}}",
            r.id,
            pct(x),
            pct(y),
            pct(w),
            pct(h)
        ));
    }
    format!("{html}<style data-duolify-regions>{css}</style>")
}

fn words(text: &str) -> HashSet<String> {
    text.split(|c: char| !c.is_alphanumeric())
        .filter(|w| w.chars().count() >= 3)
        .map(str::to_lowercase)
        .collect()
}

/// Fraction of regions whose description shares words with the text the html puts in them
/// (a diagram may have no text). Deterministic eval signal for description faithfulness.
pub fn faithfulness(html: &str, regions: &[Region]) -> f64 {
    if regions.is_empty() {
        return 0.0;
    }
    let found = elements(html);
    let faithful = regions
        .iter()
        .filter(|r| {
            let raw = found
                .get(&r.id)
                .map(|e| e.text.as_str())
                .unwrap_or_default();
            let text = words(raw);
            if text.is_empty() {
                // Short code or symbols: quoted verbatim; a diagram may have no text at all.
                return r.role == "diagram" || (!raw.is_empty() && r.description.contains(raw));
            }
            let desc = words(&r.description);
            let shared = text.intersection(&desc).count() as f64;
            let smaller = text.len().min(desc.len()).max(1) as f64;
            shared / smaller >= 0.25
        })
        .count();
    faithful as f64 / regions.len() as f64
}

/// The lesson prompt's region contract (empty when emission is off).
pub fn shape() -> String {
    let cfg = config();
    if !cfg.emit {
        return String::new();
    }
    format!(
        " Lay out every slide on fixed slots of the 16:9 stage: top (title band), body (main area), left and right (its halves), left_top, left_bottom, right_top and right_bottom (its quarters) and bottom (caption band). Put each visible block in one element that is a direct child of the root element and has data-region set to its region id; the player sizes and places it in its slot, so do not position or size those elements, never use !important, and keep the text short enough to fit. Each slide lists regions (1 to {max}), one per block: id (unique in the slide: letters, digits, - or _), slot, role (one of {roles}), description (plain text, at most {chars} characters, no markup, in the slide's language, saying what the block shows and quoting its key words) and start_ms/end_ms on the slide clock when the block is visible only part of the slide, else 0 and 0. Blocks visible at the same time use slots that do not overlap (body overlaps left, right and the quarters; left overlaps left_top and left_bottom; right overlaps right_top and right_bottom).",
        max = cfg.max_per_slide,
        roles = ROLES.join(", "),
        chars = cfg.max_description_chars,
    )
}

/// Strict JSON schema of one Slide's `regions` (structured outputs, ADR 0018).
pub fn schema() -> Value {
    let slots: Vec<&str> = config().slots.keys().map(String::as_str).collect();
    json!({"type":"array","items":{"type":"object","properties":{
        "id":{"type":"string"},
        "slot":{"type":"string","enum":slots},
        "role":{"type":"string","enum":ROLES},
        "description":{"type":"string"},
        "start_ms":{"type":"integer"},"end_ms":{"type":"integer"}},
        "required":["id","slot","role","description","start_ms","end_ms"],
        "additionalProperties":false}})
}

#[cfg(test)]
mod tests;
