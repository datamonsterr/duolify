//! Lesson and test validation rules.
use anyhow::{Result, bail};
use regex::Regex;

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
    // For short slide duration, bounds must not be impossibly exceeded
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
    let mut unique = std::collections::HashSet::new();
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

pub fn validate_lesson(v: &serde_json::Value) -> Result<()> {
    let slides = v["slides"]
        .as_array()
        .ok_or_else(|| anyhow::anyhow!("missing slides array"))?;
    if slides.is_empty() {
        bail!("lesson must contain at least one slide");
    }
    for s in slides {
        let html = s["html"].as_str().unwrap_or_default();
        let duration_ms = s["duration_ms"].as_u64().unwrap_or(3000) as u32;
        let narration = s["narration"].as_str().unwrap_or_default();
        validate_slide(html, duration_ms, narration)?;
    }

    let test = v["test"]
        .as_array()
        .ok_or_else(|| anyhow::anyhow!("missing test array"))?;
    if test.is_empty() {
        bail!("lesson must contain at least one test question");
    }
    for q in test {
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
    }
    Ok(())
}

#[cfg(test)]
pub mod tests;
