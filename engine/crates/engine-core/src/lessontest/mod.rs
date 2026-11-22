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
    // Rough speaking rate bounds: ~100 to 220 words per minute (~1.6 to 3.6 words per sec)
    let word_count = trimmed.split_whitespace().count();
    let seconds = (duration_ms as f64) / 1000.0;
    if seconds > 0.0 {
        let words_per_sec = (word_count as f64) / seconds;
        if words_per_sec > 6.0 {
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
        let duration_ms = s["duration_ms"].as_u64().unwrap_or(3000) as u32;
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
pub const LESSON_SHAPE: &str = "Return one JSON object only: {\"title\":string,\"slides\":[{\"slide_id\":string,\"html\":string,\"narration\":string,\"duration_ms\":integer,\"topic_ids\":[string]}],\"test\":[{\"question_id\":string,\"topic_id\":string,\"prompt\":string,\"choices\":[string],\"answer_index\":integer,\"explanation\":string}],\"variant_pool\":[{\"topic_id\":string,\"variants\":[{\"question_id\":string,\"topic_id\":string,\"variant_of\":string,\"prompt\":string,\"choices\":[string],\"answer_index\":integer,\"explanation\":string}]}]}. Each html is one self-contained Motion HTML slide with a single root element id=\"root\", inline CSS only: no scripts, no URLs, no HTML entities, no backslashes. narration is plain spoken text without markup; duration_ms is between 3000 and 60000 and at least 400 times the number of narration words (30 words need 12000 or more). Every slide has at least one topic_id. The test has exactly one question per distinct taught topic_id, each with a unique question_id, 2 to 6 distinct choices, a valid answer_index and an explanation. variant_pool has one entry per tested topic_id with exactly one variant: a new unique question_id, the same topic_id, variant_of equal to that topic's test question_id, a reworded prompt, and exactly the same choice texts as the source question but never in the source order: move the correct choice to another position and set answer_index to its new position. Example: source {\"question_id\":\"q1\",\"choices\":[\"Red\",\"Blue\",\"Green\"],\"answer_index\":1} -> variant {\"question_id\":\"q1-v1\",\"variant_of\":\"q1\",\"choices\":[\"Blue\",\"Green\",\"Red\"],\"answer_index\":0}. Inside html never use HTML entities (no &amp; or &nbsp;), backslashes, the words fetch, WebSocket, EventSource, XMLHttpRequest, sendBeacon or importScripts, @import, or data:, javascript:, ftp: or ws: schemes.";

/// Extra guidance for the repair turn when a slide trips the static safety check.
const SLIDE_HINT: &str = " (slide html must not contain HTML entities such as &amp; or &nbsp;, backslashes, the words fetch, WebSocket, EventSource, XMLHttpRequest, sendBeacon or importScripts, @import, or data:, javascript:, ftp: or ws: schemes)";

/// The trusted lesson prompt. `context` is the request data (outline, lesson, learner, history),
/// rendered by the caller; it stays data, below the rules.
pub fn lesson_prompt(context: &str) -> String {
    format!(
        "Generate a lesson with Motion HTML slides, explicit taught topic_ids and one question per distinct taught topic. Each question needs question_id, topic_id, prompt, choices, answer_index and explanation. Include genuine paraphrases in variant_pool linked by variant_of. {LESSON_SHAPE} Context (data): {context}"
    )
}

/// Deterministic fix-up before acceptance: a variant whose choices repeat its source question in
/// the same order is rotated with `reorder_choices` (the same rule the retry pool uses when it is
/// exhausted), so the correct text and answer stay identical. Everything else is left for
/// `validate_lesson`; `None` means nothing changed.
pub fn prepare_lesson(text: &str) -> Option<String> {
    let mut v: serde_json::Value = serde_json::from_str(text).ok()?;
    let sources: std::collections::HashMap<String, serde_json::Value> = v["test"]
        .as_array()?
        .iter()
        .filter_map(|q| Some((q["question_id"].as_str()?.to_owned(), q["choices"].clone())))
        .collect();
    let mut changed = false;
    for pool in v["variant_pool"].as_array_mut().into_iter().flatten() {
        for var in pool["variants"].as_array_mut().into_iter().flatten() {
            let Some(source) = var["variant_of"].as_str().and_then(|id| sources.get(id)) else {
                continue;
            };
            if &var["choices"] != source {
                continue;
            }
            let choices: Vec<String> = serde_json::from_value(var["choices"].clone()).ok()?;
            let index = var["answer_index"]
                .as_u64()
                .and_then(|i| u32::try_from(i).ok())?;
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
