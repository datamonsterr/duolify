//! Input guard (ADR 0025): one policy in front of every surface that sends learner text to a
//! model. Order: deterministic pre-filter (no model), then a cheap classifier, then the
//! surface's failure rule. The verdict carries a reason code only: raw text is never logged,
//! stored or put in an event. A `self_harm` verdict means a safe response and no main call.
use async_trait::async_trait;
use regex::Regex;
use serde::{Deserialize, Serialize};
use std::{sync::OnceLock, time::Duration};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Reason {
    PromptInjection,
    UnsafeContent,
    SelfHarm,
    Pii,
    OffTopicForCourse,
    TooLong,
}

/// Guard contract version returned on the wire.
pub const VERSION: &str = "guard-v1";

impl Source {
    pub fn code(self) -> &'static str {
        match self {
            Source::PreFilter => "pre_filter",
            Source::Model => "model",
            Source::Failure => "failure",
        }
    }
}

impl Reason {
    /// Every code, in the order of the verdict schema.
    pub const ALL: [Reason; 6] = [
        Reason::PromptInjection,
        Reason::UnsafeContent,
        Reason::SelfHarm,
        Reason::Pii,
        Reason::OffTopicForCourse,
        Reason::TooLong,
    ];
    pub fn from_code(code: &str) -> Option<Reason> {
        Self::ALL.into_iter().find(|r| r.code() == code)
    }
    pub fn code(self) -> &'static str {
        match self {
            Reason::PromptInjection => "prompt_injection",
            Reason::UnsafeContent => "unsafe_content",
            Reason::SelfHarm => "self_harm",
            Reason::Pii => "pii",
            Reason::OffTopicForCourse => "off_topic_for_course",
            Reason::TooLong => "too_long",
        }
    }
}

/// Where the text goes. Each surface has its own length bound, latency budget and failure rule.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Surface {
    CourseTopic,
    Steer,
    OutlineChat,
    Character,
    CoursePublish,
    Review,
    QuickNote,
    TutorChat,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OnFailure {
    /// Creation and publication: no verdict means no generation.
    Closed,
    /// Chat, notes, transcripts: the deterministic pre-filter already ran; the text stays
    /// delimited data in the main prompt.
    Open,
}

impl Surface {
    pub fn from_code(code: &str) -> Option<Surface> {
        Some(match code {
            "course_topic" => Surface::CourseTopic,
            "steer" => Surface::Steer,
            "outline_chat" => Surface::OutlineChat,
            "character" => Surface::Character,
            "course_publish" => Surface::CoursePublish,
            "review" => Surface::Review,
            "quick_note" => Surface::QuickNote,
            "tutor_chat" => Surface::TutorChat,
            _ => return None,
        })
    }
    pub fn on_failure(self) -> OnFailure {
        match self {
            Surface::QuickNote | Surface::TutorChat => OnFailure::Open,
            _ => OnFailure::Closed,
        }
    }
    /// How long the caller waits for the classifier (spec: 4 s before generation, 1 s hold on
    /// tutor chat).
    pub fn budget(self) -> Duration {
        match self {
            Surface::TutorChat => Duration::from_millis(1000),
            Surface::QuickNote => Duration::from_secs(4),
            _ => Duration::from_secs(4),
        }
    }
    pub fn max_chars(self) -> usize {
        match self {
            Surface::Steer | Surface::Character => 2_000,
            Surface::TutorChat | Surface::OutlineChat => 4_000,
            _ => 8_000,
        }
    }
}

/// Where a decision came from, for counters (`guard_source`).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Source {
    PreFilter,
    Model,
    /// The model failed and the surface's failure rule decided.
    Failure,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Decision {
    pub allowed: bool,
    pub reason: Option<Reason>,
    pub source: Source,
}

impl Decision {
    pub fn allow(source: Source) -> Self {
        Self {
            allowed: true,
            reason: None,
            source,
        }
    }
    pub fn block(reason: Reason, source: Source) -> Self {
        Self {
            allowed: false,
            reason: Some(reason),
            source,
        }
    }
    /// Self-harm gets the safe response (no main model call, no charge), not a refusal.
    pub fn safe_response(&self) -> bool {
        self.reason == Some(Reason::SelfHarm)
    }
}

/// The classifier: an `input_guard` core plus the surface skill, on the cheapest model that
/// meets the guard golden set. `Err` is an outage or a malformed answer.
#[async_trait]
pub trait GuardModel: Send + Sync {
    async fn classify(&self, surface: Surface, text: &str) -> anyhow::Result<Option<Reason>>;
}

fn re(cell: &'static OnceLock<Regex>, pattern: &str) -> &'static Regex {
    cell.get_or_init(|| Regex::new(pattern).expect("guard pattern"))
}

/// Phrases that try to override the trusted prompt, English and Vietnamese. A match decides
/// without a model; the list is conservative (benign look-alikes such as "explain what a
/// system prompt is" must pass, which the golden set checks).
fn injection(text: &str) -> bool {
    static R: OnceLock<Regex> = OnceLock::new();
    re(
        &R,
        r"(?i)(ignore|disregard|forget)\s+(all\s+|any\s+|the\s+|your\s+)*(previous|prior|above|earlier)\s+(instructions?|rules?|prompts?)|(reveal|print|show|repeat)\s+(me\s+)?(your|the)\s+(system|hidden)\s+prompt|\byou\s+are\s+now\s+(dan|in\s+developer\s+mode)\b|bỏ\s+qua\s+(mọi|tất\s+cả)\s+(hướng\s+dẫn|chỉ\s+dẫn)\s+(trước|ở\s+trên)",
    )
    .is_match(text)
}

/// Zero-width, bidi-override and other invisible controls used to hide instructions.
fn hidden_chars(text: &str) -> bool {
    text.chars().any(|c| {
        matches!(c,
            '\u{200B}'..='\u{200F}' | '\u{202A}'..='\u{202E}' | '\u{2060}'..='\u{2064}' | '\u{2066}'..='\u{2069}' | '\u{FEFF}')
            || (c.is_control() && !matches!(c, '\n' | '\r' | '\t'))
    })
}

fn luhn(digits: &str) -> bool {
    let d: Vec<u32> = digits.chars().filter_map(|c| c.to_digit(10)).collect();
    if !(13..=19).contains(&d.len()) {
        return false;
    }
    d.iter()
        .rev()
        .enumerate()
        .map(|(i, &x)| {
            if i % 2 == 1 {
                let y = x * 2;
                if y > 9 { y - 9 } else { y }
            } else {
                x
            }
        })
        .sum::<u32>()
        % 10
        == 0
}

fn card_regex() -> &'static Regex {
    static R: OnceLock<Regex> = OnceLock::new();
    re(&R, r"\b\d(?:[ -]?\d){12,18}\b")
}

fn secret_regex() -> &'static Regex {
    static R: OnceLock<Regex> = OnceLock::new();
    re(
        &R,
        r"\b(?:sk-[A-Za-z0-9_-]{20,}|AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{30,}|xox[abp]-[A-Za-z0-9-]{20,}|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,})\b",
    )
}

fn has_card(text: &str) -> bool {
    card_regex().find_iter(text).any(|m| luhn(m.as_str()))
}

/// Cards and secret-like tokens replaced by placeholders in a prompt copy (D13). Emails and
/// phones are left alone: redacting them breaks tutoring and note content.
pub fn redact(text: &str) -> String {
    let t = card_regex().replace_all(text, |c: &regex::Captures| {
        if luhn(&c[0]) {
            "[card]".to_string()
        } else {
            c[0].to_string()
        }
    });
    secret_regex().replace_all(&t, "[secret]").into_owned()
}

/// The deterministic pre-filter. `Some` decides without a model.
pub fn pre_filter(surface: Surface, text: &str) -> Option<Reason> {
    if text.chars().count() > surface.max_chars() {
        return Some(Reason::TooLong);
    }
    if hidden_chars(text) || injection(text) {
        return Some(Reason::PromptInjection);
    }
    if has_card(text) || secret_regex().is_match(text) {
        return Some(Reason::Pii);
    }
    None
}

/// Parses the classifier's answer: `{"allowed":true}` or `{"allowed":false,"reason_code":"..."}`.
/// Anything else (prose, unknown reason, missing code on a block) is an error, never a pass.
pub fn parse_verdict(text: &str) -> anyhow::Result<Option<Reason>> {
    #[derive(Deserialize)]
    #[serde(deny_unknown_fields)]
    struct V {
        allowed: bool,
        reason_code: Option<String>,
    }
    let v: V = serde_json::from_str(text.trim())?;
    // The strict output schema uses "" for no reason; null and absent mean the same.
    let code = v.reason_code.filter(|c| !c.is_empty());
    match (v.allowed, code) {
        (true, _) => Ok(None),
        (false, Some(c)) => Ok(Some(
            Reason::from_code(&c).ok_or_else(|| anyhow::anyhow!("unknown reason_code"))?,
        )),
        (false, None) => anyhow::bail!("block without reason_code"),
    }
}

/// Maps a classifier verdict (`None` = outage or malformed) through the surface failure rule.
impl Decision {
    pub fn from_verdict(surface: Surface, verdict: Option<Option<Reason>>) -> Self {
        match verdict {
            Some(None) => Decision::allow(Source::Model),
            Some(Some(r)) => Decision::block(r, Source::Model),
            None => match surface.on_failure() {
                OnFailure::Open => Decision::allow(Source::Failure),
                // Closed surfaces use the generic reason; the caller shows its retry message.
                OnFailure::Closed => Decision::block(Reason::UnsafeContent, Source::Failure),
            },
        }
    }
}

/// The whole guard for one text. Never panics, never logs `text`.
pub async fn check(surface: Surface, text: &str, model: Option<&dyn GuardModel>) -> Decision {
    if let Some(r) = pre_filter(surface, text) {
        return Decision::block(r, Source::PreFilter);
    }
    let verdict = match model {
        Some(m) => tokio::time::timeout(surface.budget(), m.classify(surface, text))
            .await
            .ok()
            .and_then(Result::ok),
        None => None,
    };
    Decision::from_verdict(surface, verdict)
}

pub mod classifier;

#[cfg(test)]
mod tests;
