//! Lesson and test validation rules.
use anyhow::{Result, bail};
use regex::Regex;
use std::collections::HashSet;

pub const MIN_CHOICES: usize = 2;
pub const MAX_CHOICES: usize = 6;
pub const MIN_DURATION_MS: u32 = 1000;
pub const MAX_DURATION_MS: u32 = 60000;

pub fn validate_narration(narration: &str, duration_ms: u32) -> Result<()> {
    let trimmed = narration.trim();
    if trimmed.is_empty() {
        bail!("narration text must not be empty");
    }
    // Reject HTML tags or unspoken markup
    let tag_re = Regex::new(r"<[^>]+>").unwrap();
    if tag_re.is_match(trimmed) {
        bail!("narration text must not contain markup");
    }
    // Bound narration to at most 3.3 words per second.
    let word_count = trimmed.split_whitespace().count();
    let seconds = (duration_ms as f64) / 1000.0;
    if seconds > 0.0 {
        let words_per_sec = (word_count as f64) / seconds;
        if words_per_sec > 3.3 {
            bail!("narration length exceeds slide duration bounds");
        }
    }
    Ok(())
}

pub fn validate_slide(html: &str, duration_ms: u32, narration: &str) -> Result<()> {
    if !(MIN_DURATION_MS..=MAX_DURATION_MS).contains(&duration_ms) {
        bail!("duration_ms out of allowed bounds [1000, 60000]");
    }
    crate::slide::validate(html, &[]).map_err(|e| anyhow::anyhow!("{e}"))?;
    validate_narration(narration, duration_ms)?;
    Ok(())
}

pub fn validate_slide_topics(topic_ids: &[String]) -> Result<()> {
    if topic_ids.is_empty() {
        bail!("slide must teach at least one topic");
    }
    for t in topic_ids {
        if t.trim().is_empty() {
            bail!("topic_id cannot be empty");
        }
    }
    Ok(())
}

pub fn validate_question(
    prompt: &str,
    choices: &[String],
    answer_index: u32,
    explanation: &str,
) -> Result<()> {
    if prompt.trim().is_empty() {
        bail!("question prompt must not be empty");
    }
    if choices.len() < MIN_CHOICES || choices.len() > MAX_CHOICES {
        bail!("choices count must be between 2 and 6");
    }
    for (i, c) in choices.iter().enumerate() {
        if c.trim().is_empty() {
            bail!("choice {i} must not be empty");
        }
    }
    // Check for duplicates
    let mut unique = HashSet::new();
    for c in choices {
        let norm = c.trim().to_lowercase();
        if !unique.insert(norm) {
            bail!("choices must not contain duplicates");
        }
    }
    if (answer_index as usize) >= choices.len() {
        bail!("answer_index out of bounds");
    }
    if explanation.trim().is_empty() {
        bail!("explanation must not be empty");
    }
    Ok(())
}

pub fn validate_question_with_topic(
    topic_id: &str,
    prompt: &str,
    choices: &[String],
    answer_index: u32,
    explanation: &str,
) -> Result<()> {
    if topic_id.trim().is_empty() {
        bail!("question topic_id must not be empty");
    }
    validate_question(prompt, choices, answer_index, explanation)
}

pub fn normalize_answer(s: &str) -> String {
    s.chars()
        .filter(|c| c.is_alphanumeric() || c.is_whitespace())
        .collect::<String>()
        .to_lowercase()
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
}

#[allow(clippy::too_many_arguments)]
pub fn validate_variant(
    source_topic_id: &str,
    source_question_id: &str,
    source_prompt: &str,
    source_choices: &[String],
    source_answer_index: u32,
    variant_prompt: &str,
    variant_choices: &[String],
    variant_answer_index: u32,
    variant_explanation: &str,
    previous_variant_prompts: &[String],
) -> Result<()> {
    if source_topic_id.trim().is_empty() {
        bail!("source topic_id is empty");
    }
    if source_question_id.trim().is_empty() {
        bail!("source question_id is empty");
    }
    if variant_prompt.trim().is_empty() {
        bail!("variant prompt is empty");
    }
    if variant_prompt
        .trim()
        .eq_ignore_ascii_case(source_prompt.trim())
    {
        bail!("variant prompt must differ from source question");
    }
    let lower = variant_prompt.trim().to_lowercase();
    if lower.contains("(rephrased)") || lower.starts_with("in other words:") {
        bail!("variant must be a real paraphrase");
    }
    for prev in previous_variant_prompts {
        if variant_prompt.trim().eq_ignore_ascii_case(prev.trim()) {
            bail!("variant prompt is duplicate of previous variant");
        }
    }
    validate_question(
        variant_prompt,
        variant_choices,
        variant_answer_index,
        variant_explanation,
    )?;

    // Choices must differ in order or text
    if variant_choices == source_choices {
        bail!("variant choices order must differ from source question");
    }

    // Semantic equality check of the correct answers after normalization
    if (source_answer_index as usize) < source_choices.len()
        && (variant_answer_index as usize) < variant_choices.len()
    {
        let source_ans = normalize_answer(&source_choices[source_answer_index as usize]);
        let variant_ans = normalize_answer(&variant_choices[variant_answer_index as usize]);
        let source_tokens: HashSet<&str> = source_ans.split_whitespace().collect();
        let variant_tokens: HashSet<&str> = variant_ans.split_whitespace().collect();
        let common = source_tokens.intersection(&variant_tokens).count();
        if common == 0 && source_ans != variant_ans {
            // If neither exact match nor word overlap, check substring
            if !variant_ans.contains(&source_ans) && !source_ans.contains(&variant_ans) {
                bail!("variant correct answer must be semantically equal to source answer");
            }
        }
    }

    Ok(())
}

pub fn validate_lesson(v: &serde_json::Value) -> Result<()> {
    let slides = v["slides"]
        .as_array()
        .ok_or_else(|| anyhow::anyhow!("missing slides array"))?;
    if slides.is_empty() {
        bail!("lesson must contain at least one slide");
    }

    let mut taught_topics: HashSet<String> = HashSet::new();

    for s in slides {
        let html = s["html"].as_str().unwrap_or_default();
        let duration_ms = match s["duration_ms"].as_u64() {
            Some(n) => u32::try_from(n)
                .map_err(|_| anyhow::anyhow!("duration_ms out of allowed bounds [1000, 60000]"))?,
            None => 3000,
        };
        let narration = s["narration"].as_str().unwrap_or_default();
        validate_slide(html, duration_ms, narration)?;

        let topic_ids: Vec<String> = s["topic_ids"]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(|t| t.as_str().map(str::to_owned))
            .collect();
        validate_slide_topics(&topic_ids)?;
        for t in topic_ids {
            taught_topics.insert(t);
        }
    }

    let test = v["test"]
        .as_array()
        .ok_or_else(|| anyhow::anyhow!("missing test array"))?;
    if test.is_empty() {
        bail!("lesson must contain at least one test question");
    }

    let mut questions_by_topic: std::collections::HashMap<String, &serde_json::Value> =
        std::collections::HashMap::new();

    let mut question_ids = HashSet::new();
    for q in test {
        let id = q["question_id"].as_str().unwrap_or_default();
        if id.trim().is_empty() || !question_ids.insert(id.to_string()) {
            bail!("source question_id must be nonempty and unique");
        }

        let topic_id = q["topic_id"].as_str().unwrap_or_default();
        if topic_id.trim().is_empty() {
            bail!("question must have a non-empty topic_id");
        }
        if !taught_topics.contains(topic_id) {
            bail!("question topic '{topic_id}' is not taught by any slide in the lesson");
        }

        let prompt = q["prompt"].as_str().unwrap_or_default();
        let choices: Vec<String> = q["choices"]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(|c| c.as_str().map(str::to_owned))
            .collect();
        let answer_index = q["answer_index"].as_u64().unwrap_or(0) as u32;
        let explanation = q["explanation"].as_str().unwrap_or_default();
        validate_question(prompt, &choices, answer_index, explanation)?;

        if questions_by_topic.insert(topic_id.to_string(), q).is_some() {
            bail!("each source question must have a distinct topic_id");
        }
    }

    // Validate variant pool if present
    if let Some(pool) = v["variant_pool"].as_array() {
        for tp in pool {
            let pool_topic = tp["topic_id"].as_str().unwrap_or_default();
            if !taught_topics.contains(pool_topic) {
                bail!("variant pool topic '{pool_topic}' is not taught by any slide");
            }
            let source_q = match questions_by_topic.get(pool_topic) {
                Some(q) => *q,
                None => bail!("variant pool topic '{pool_topic}' has no source question in test"),
            };
            let source_prompt = source_q["prompt"].as_str().unwrap_or_default();
            let source_choices: Vec<String> = source_q["choices"]
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(|c| c.as_str().map(str::to_owned))
                .collect();
            let source_answer_index = source_q["answer_index"].as_u64().unwrap_or(0) as u32;

            if let Some(variants) = tp["variants"].as_array() {
                let mut prev_prompts: Vec<String> = Vec::new();
                let mut variant_ids = HashSet::new();
                for var in variants {
                    let id = var["question_id"].as_str().unwrap_or_default();
                    if id.trim().is_empty()
                        || question_ids.contains(id)
                        || !variant_ids.insert(id.to_string())
                        || var["topic_id"].as_str() != Some(pool_topic)
                        || var["variant_of"].as_str() != source_q["question_id"].as_str()
                    {
                        bail!(
                            "variant identity or topic linkage mismatch: variant {id:?} needs a new question_id not used by any test question or other variant, topic_id {pool_topic:?} and variant_of {:?}",
                            source_q["question_id"].as_str().unwrap_or_default()
                        );
                    }

                    let v_prompt = var["prompt"].as_str().unwrap_or_default();
                    let v_choices: Vec<String> = var["choices"]
                        .as_array()
                        .into_iter()
                        .flatten()
                        .filter_map(|c| c.as_str().map(str::to_owned))
                        .collect();
                    let v_ans_idx = var["answer_index"].as_u64().unwrap_or(0) as u32;
                    let v_exp = var["explanation"].as_str().unwrap_or_default();

                    validate_variant(
                        pool_topic,
                        source_q["question_id"].as_str().unwrap_or_default(),
                        source_prompt,
                        &source_choices,
                        source_answer_index,
                        v_prompt,
                        &v_choices,
                        v_ans_idx,
                        v_exp,
                        &prev_prompts,
                    )
                    .map_err(|e| {
                        // Name the variant and the source answer so a repair can fix exactly it.
                        anyhow::anyhow!(
                            "variant {id} of {}: {e}; the source choices are {:?} with correct choice {:?}",
                            source_q["question_id"].as_str().unwrap_or_default(),
                            source_choices,
                            source_choices
                                .get(source_answer_index as usize)
                                .map(String::as_str)
                                .unwrap_or_default()
                        )
                    })?;
                    prev_prompts.push(v_prompt.to_string());
                }
            }
        }
    }

    Ok(())
}

/// The JSON contract every generated lesson answers (`validate_lesson` enforces it).
pub const LESSON_SHAPE: &str = "Return one JSON object with title, slides (slide_id, html, narration, duration_ms, topic_ids), test and variant_pool (topic_id, variants). Teach one objective: start with prerequisite recall or a hook question, then a concept, worked example and check; end with a summary. Each wrong choice names a misconception; explain why for every question. At most 40 visible words per slide. Narration explains rather than reads the slide; allow at least 330 ms per narration word. Slide html has one root element id=\"root\" and inline CSS only: no scripts, URLs, HTML entities or backslashes. duration_ms is 3000 to 60000. Test: one question per taught topic_id, unique question_ids, 2 to 6 different choices. variant_pool: one variant per tested topic with a new question_id, variant_of set to the test question_id, a reworded prompt, the same choice texts in a different order and answer_index on the same correct text.";

/// Extra guidance for the repair turn when a slide trips the static safety check.
const SLIDE_HINT: &str = " (slide html must not contain HTML entities such as &amp; or &nbsp;, backslashes, the words fetch, WebSocket, EventSource, XMLHttpRequest, sendBeacon or importScripts, @import, or data:, javascript:, ftp: or ws: schemes)";

/// The trusted lesson prompt. `context` is the request data (outline, lesson, learner, history),
/// rendered by the caller; it stays data, below the rules.
pub fn lesson_prompt(context: &str) -> String {
    format!("{} Context (data): {context}", lesson_rules())
}

/// The always-on lesson rules, no data: the whole monolithic prompt before its context and the
/// core of a routed one (`skills`).
fn lesson_rules() -> String {
    format!(
        "Generate a lesson with Motion HTML slides, explicit taught topic_ids and one question per distinct taught topic. Use lesson_length from context: short 3 slides and 2 questions, medium 5 slides and 3 questions, long 8 slides and 5 questions; default medium. Each question needs question_id, topic_id, prompt, choices, answer_index and explanation. Include genuine paraphrases in variant_pool linked by variant_of. {LESSON_SHAPE}{regions}",
        regions = crate::regions::shape()
    )
}

/// Skills decided by rule, never by the model: narration pacing always, then locale, cast and
/// learner level. (`steering_focus` is not a skill: steering already rides in the policy.)
pub const RULE_SKILLS: &[&str] = &[
    "narration_pacing",
    "language_practice",
    "character_cast",
    "age_band_tone",
];

/// Rule-decided skill ids for a routed lesson, in priority order.
pub fn lesson_skill_ids(locale: &str, has_cast: bool, level: &str) -> Vec<String> {
    let mut ids = vec!["narration_pacing".to_string()];
    if !locale.is_empty() && !locale.to_ascii_lowercase().starts_with("en") {
        ids.push("language_practice".to_string());
    }
    if has_cast {
        ids.push("character_cast".to_string());
    }
    if !level.trim().is_empty() {
        ids.push("age_band_tone".to_string());
    }
    ids
}

/// Rules, cache, decision model, then default set for a lesson. Never fails, never blocks
/// longer than the router timeout.
pub async fn select_lesson_skills(
    locale: &str,
    has_cast: bool,
    level: &str,
    title: &str,
    goal: &str,
    topic: &str,
) -> crate::router::Selection {
    let input = crate::router::RouterInput {
        task: "lesson_slides".into(),
        title: title.into(),
        goal: goal.into(),
        topic: topic.into(),
        level: level.into(),
        locale: locale.into(),
        flags: if has_cast {
            vec!["cast".into()]
        } else {
            vec![]
        },
    };
    crate::router::select(
        crate::skills::bundled(),
        "lesson_slides",
        lesson_skill_ids(locale, has_cast, level),
        RULE_SKILLS,
        &Default::default(),
        &input,
        crate::router::installed(),
        crate::router::shared_cache(),
        crate::router::shared_breaker(),
        crate::router::TIMEOUT,
    )
    .await
}

/// The routed lesson prompt: `system` is the byte-stable core plus the picked skills, `user` the
/// context data (untrusted, so never in `system`). Same rules as `lesson_prompt`, in two parts.
pub fn routed_lesson_prompt(
    context: &str,
    ids: &[String],
) -> Result<crate::skills::Prompt, crate::skills::SkillError> {
    routed_lesson_prompt_in(crate::skills::bundled(), context, ids)
}

/// The bundled skills with `lesson_slides` forced to `routed`, for the evals' routed arm (the
/// shipped switch stays `monolithic` until the rollout PR).
pub fn routed_eval_library() -> crate::skills::Library {
    let sources: Vec<(&str, String)> = crate::skills::BUNDLED
        .iter()
        .map(|(f, t)| {
            (
                *f,
                t.replace("prompt_mode = \"monolithic\"", "prompt_mode = \"routed\""),
            )
        })
        .collect();
    let refs: Vec<(&str, &str)> = sources.iter().map(|(f, t)| (*f, t.as_str())).collect();
    crate::skills::Library::from_sources(&refs).expect("bundled skills are valid")
}

/// `routed_lesson_prompt` against an explicit library (tests, evals).
pub fn routed_lesson_prompt_in(
    lib: &crate::skills::Library,
    context: &str,
    ids: &[String],
) -> Result<crate::skills::Prompt, crate::skills::SkillError> {
    lib.assemble(
        "lesson_slides",
        &lesson_rules(),
        ids,
        &format!("Context (data): {context}"),
    )
}

/// A request as compact JSON for a routed prompt: empty, zero and false values are dropped (they
/// carry no information and the prost `Debug` rendering spells every one out), and `limits` (the
/// quote's volatile token and price bounds) is not for the model. Keys keep their order.
pub fn compact_context(request: &serde_json::Value) -> String {
    fn strip(v: &mut serde_json::Value) -> bool {
        match v {
            serde_json::Value::Null => true,
            serde_json::Value::Bool(b) => !*b,
            serde_json::Value::Number(n) => n.as_f64() == Some(0.0),
            serde_json::Value::String(s) => s.is_empty(),
            serde_json::Value::Array(a) => {
                a.iter_mut().for_each(|x| {
                    strip(x);
                });
                a.is_empty()
            }
            serde_json::Value::Object(o) => {
                o.retain(|_, x| !strip(x));
                o.is_empty()
            }
        }
    }
    let mut v = request.clone();
    if let Some(o) = v.as_object_mut() {
        o.remove("limits");
    }
    strip(&mut v);
    v.to_string()
}

/// The allowlisted lesson prompt context, with only recent history.
pub fn lesson_context(
    request: &serde_json::Value,
    lesson_length: Option<&str>,
) -> serde_json::Value {
    let history = request["history"].as_array().cloned().unwrap_or_default();
    serde_json::json!({
        "outline": request["outline"], "lesson": request["lesson"], "learner": request["learner"],
        "history": &history[history.len().saturating_sub(5)..],
        "lesson_length": lesson_length.unwrap_or("medium")
    })
}

/// Heuristic quality signals only; these never reject a generated lesson.
pub fn quality_warnings(lesson: &serde_json::Value) -> Vec<&'static str> {
    let style = Regex::new(r"(?is)<(?:style|script)\b[^>]*>.*?</(?:style|script)\s*>").unwrap();
    let tags = Regex::new(r"<[^>]*>").unwrap();
    let mut warnings = Vec::new();
    for slide in lesson["slides"].as_array().into_iter().flatten() {
        let html = slide["html"].as_str().unwrap_or_default();
        let stripped = style.replace_all(html, " ");
        let visible = tags.replace_all(&stripped, " ");
        let screen: Vec<String> = visible
            .split(|c: char| !c.is_alphanumeric())
            .filter(|s| !s.is_empty())
            .map(str::to_lowercase)
            .collect();
        let narration = slide["narration"].as_str().unwrap_or_default();
        let spoken: Vec<String> = narration
            .split(|c: char| !c.is_alphanumeric())
            .filter(|s| !s.is_empty())
            .map(str::to_lowercase)
            .collect();
        let duration = slide["duration_ms"].as_f64().unwrap_or(0.0);
        let wpm = spoken.len() as f64 * 60000.0 / duration.max(1.0);
        let overlap = spoken.iter().filter(|w| screen.contains(w)).count() as f64
            / spoken.len().max(1) as f64;
        for (bad, code) in [
            (screen.len() > 40, "visible_words"),
            (!(110.0..=200.0).contains(&wpm), "narration_wpm"),
            (overlap > 0.6, "narration_overlap"),
        ] {
            if bad && !warnings.contains(&code) {
                warnings.push(code);
            }
        }
    }
    warnings
}

/// The lesson request as prompt data: today's `Debug` rendering while `lesson_slides` is
/// monolithic (byte-identical prompts), compact JSON once the task is routed.
pub fn render_context<T: std::fmt::Debug + serde::Serialize>(context: &T) -> String {
    if crate::skills::bundled().mode("lesson_slides") == crate::skills::PromptMode::Routed
        && let Ok(v) = serde_json::to_value(context)
    {
        return compact_context(&v);
    }
    format!("{context:?}")
}

/// Narration pace the lesson prompt asks for (`LESSON_SHAPE`), enforced by `prepare_lesson`.
const MIN_MS_PER_WORD: u64 = 400;

/// Deterministic fix-up before acceptance: each slide's `duration_ms` is raised to 400 ms per
/// narration word (at most the 60 s slide maximum); a variant whose choices repeat its source question in
/// the same order is rotated with `reorder_choices` (the same rule the retry pool uses when it is
/// exhausted), so the correct text and answer stay identical; a variant whose choices are a
/// reordering of the source choices gets `answer_index` pointing at the source's correct text.
/// Everything else is left for `validate_lesson`; `None` means nothing changed.
pub fn prepare_lesson(text: &str) -> Option<String> {
    let mut v: serde_json::Value = serde_json::from_str(text).ok()?;
    let sources: std::collections::HashMap<String, (serde_json::Value, serde_json::Value)> =
        v["test"]
            .as_array()?
            .iter()
            .filter_map(|q| {
                let correct = q["choices"]
                    .get(q["answer_index"].as_u64()? as usize)?
                    .clone();
                Some((
                    q["question_id"].as_str()?.to_owned(),
                    (q["choices"].clone(), correct),
                ))
            })
            .collect();
    let mut changed = false;
    // Speaking time is arithmetic, not judgement: raise each slide to the rate the prompt asks for.
    let slides = v
        .get_mut("slides")
        .and_then(serde_json::Value::as_array_mut);
    for slide in slides.into_iter().flatten() {
        let words = slide["narration"]
            .as_str()
            .map_or(0, |n| n.split_whitespace().count() as u64);
        let wanted = (words * MIN_MS_PER_WORD).min(u64::from(MAX_DURATION_MS));
        if slide["duration_ms"].as_u64().is_some_and(|d| d < wanted) {
            slide["duration_ms"] = serde_json::json!(wanted);
            changed = true;
        }
    }
    // Provider JSON is unvalidated here: only non-panicking accessors (`get_mut`), so a
    // malformed pool is left for `validate_lesson` to reject.
    let pools = v
        .get_mut("variant_pool")
        .and_then(serde_json::Value::as_array_mut);
    for pool in pools.into_iter().flatten() {
        let variants = pool
            .get_mut("variants")
            .and_then(serde_json::Value::as_array_mut);
        for var in variants.into_iter().flatten() {
            if !var.is_object() {
                continue;
            }
            let Some((source, correct)) = var["variant_of"].as_str().and_then(|id| sources.get(id))
            else {
                continue;
            };
            // A reordered copy of the source choices must mark the source's correct text.
            let sorted = |c: &serde_json::Value| {
                let mut c: Vec<String> = serde_json::from_value(c.clone()).unwrap_or_default();
                c.sort();
                c
            };
            if &var["choices"] != source && sorted(&var["choices"]) == sorted(source) {
                let Some(at) = var["choices"]
                    .as_array()
                    .and_then(|c| c.iter().position(|c| c == correct))
                else {
                    continue;
                };
                if var["answer_index"] != serde_json::json!(at) {
                    var["answer_index"] = serde_json::json!(at);
                    changed = true;
                }
                continue;
            }
            if &var["choices"] != source {
                continue;
            }
            let Ok(choices) = serde_json::from_value::<Vec<String>>(var["choices"].clone()) else {
                continue;
            };
            // Rotate around the source's correct text, whatever index the variant claimed.
            let Some(index) = choices
                .iter()
                .position(|c| correct == c)
                .and_then(|i| u32::try_from(i).ok())
            else {
                continue;
            };
            if let Ok((rotated, new_index)) = reorder_choices(&choices, index) {
                var["choices"] = serde_json::json!(rotated);
                var["answer_index"] = serde_json::json!(new_index);
                changed = true;
            }
        }
    }
    changed.then(|| v.to_string())
}

/// Production acceptance of one generated lesson, with the reason quoted in the one repair.
pub fn check_generated(
    text: &str,
    instructions: &[crate::steering::Instruction],
    cast: &str,
) -> std::result::Result<(), String> {
    if let Some(code) = crate::steering::lesson_output_violation(text, instructions) {
        return Err(format!("the lesson was rejected as {code}"));
    }
    let v: serde_json::Value = serde_json::from_str(text).map_err(|e| e.to_string())?;
    validate_lesson(&v).map_err(|e| {
        let e = e.to_string();
        if e.contains("obfuscation") {
            let found = v["slides"]
                .as_array()
                .into_iter()
                .flatten()
                .find_map(|s| crate::slide::dangerous_match(s["html"].as_str().unwrap_or_default()))
                .map(|m| format!(", found {m:?}"))
                .unwrap_or_default();
            format!("{e}{found}{SLIDE_HINT}")
        } else if e.contains("narration length") {
            format!(
                "{e} (set each slide's duration_ms to at least 400 times its narration word count)"
            )
        } else if e.contains("semantically equal") {
            format!("{e} (reuse the source question's exact correct choice text in each variant)")
        } else {
            e
        }
    })?;
    crate::regions::validate_lesson(&v)?;
    if !cast.is_empty() {
        crate::characters::validate_lesson_segments(&v, cast).map_err(|e| e.to_string())?;
    }
    Ok(())
}

pub fn validate_history(history: &[String]) -> Result<()> {
    for (i, item) in history.iter().enumerate() {
        if item.trim().is_empty() {
            bail!("history item {i} must not be empty");
        }
    }
    Ok(())
}

/// Cyclically shifts choices and calculates the updated answer index for retry pool exhaustion.
pub fn reorder_choices(choices: &[String], answer_index: u32) -> Result<(Vec<String>, u32)> {
    let n = choices.len();
    if !(MIN_CHOICES..=MAX_CHOICES).contains(&n) {
        bail!("choices count must be between {MIN_CHOICES} and {MAX_CHOICES}");
    }
    if (answer_index as usize) >= n {
        bail!("answer_index out of bounds");
    }
    let mut reordered = Vec::with_capacity(n);
    for i in 0..n {
        reordered.push(choices[(i + 1) % n].clone());
    }
    let new_index = ((answer_index as usize + n - 1) % n) as u32;
    Ok((reordered, new_index))
}

/// Validates safe fallback when a retry pool is exhausted or provider is unavailable.
pub fn validate_exhausted_fallback_variant(
    source_choices: &[String],
    source_answer_index: u32,
    fallback_choices: &[String],
    fallback_answer_index: u32,
) -> Result<()> {
    if source_choices.len() != fallback_choices.len() {
        bail!("fallback choices length must match source choices length");
    }
    if fallback_choices == source_choices {
        bail!("fallback choices must be reordered from source");
    }
    if (source_answer_index as usize) >= source_choices.len() {
        bail!("source answer index out of bounds");
    }
    if (fallback_answer_index as usize) >= fallback_choices.len() {
        bail!("fallback answer index out of bounds");
    }
    let src_ans = normalize_answer(&source_choices[source_answer_index as usize]);
    let fb_ans = normalize_answer(&fallback_choices[fallback_answer_index as usize]);
    if src_ans != fb_ans {
        bail!("fallback correct choice must be identical to source correct choice");
    }
    Ok(())
}

#[cfg(test)]
pub mod tests;

pub mod retry;
