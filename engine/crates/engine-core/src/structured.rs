//! Structured JSON output shared by every JSON-returning task: one extraction rule, the
//! per-task OpenRouter `response_format`, and the corrective message for the one bounded repair.
//!
//! Extraction is deliberately narrow: surrounding whitespace and a single surrounding Markdown
//! code fence are removed, nothing else. Prose, partial JSON and "key: value" answers stay
//! invalid, so a safety verdict is only ever read from schema-valid JSON.
use serde_json::{Value, json};

/// The JSON body of a model answer: trims whitespace and strips one surrounding code fence
/// (```` ``` ```` or ```` ```json ````). Anything else is returned trimmed and unchanged.
pub fn extract_json(raw: &str) -> &str {
    let text = raw.trim();
    let Some(rest) = text.strip_prefix("```") else {
        return text;
    };
    let Some((tag, body)) = rest.split_once('\n') else {
        return text;
    };
    // Only a bare language tag may follow the opening fence.
    if !tag.trim().chars().all(|c| c.is_ascii_alphanumeric()) {
        return text;
    }
    let Some(body) = body.trim_end().strip_suffix("```") else {
        return text;
    };
    // One fence only: an inner fence means this is not a single fenced block.
    if body.contains("```") {
        return text;
    }
    body.trim()
}

/// Closed reason codes of the steering screen (`steering::verdict`).
pub const STEER_REASONS: [&str; 4] = [
    "prompt_injection",
    "unsafe_content",
    "off_topic_for_course",
    "too_long",
];
/// Closed reason codes of the Character screen (`characters::verdict`).
pub const CHARACTER_REASONS: [&str; 5] = [
    "real_person",
    "third_party_ip",
    "sexual_content",
    "age_inappropriate",
    "prompt_injection",
];

fn schema(name: &str, schema: Value) -> Value {
    json!({"type":"json_schema","json_schema":{"name":name,"strict":true,"schema":schema}})
}

fn with_empty(codes: &[&str]) -> Vec<String> {
    std::iter::once("")
        .chain(codes.iter().copied())
        .map(str::to_owned)
        .collect()
}

/// OpenRouter `response_format` per task (see `engine/config/models.toml`). Tasks with a small,
/// closed answer get strict `json_schema`; large generation tasks get `json_object` and keep their
/// own validators; prose tasks (`chat`, `setup_chat`) get none. Both production models list
/// `response_format` and `structured_outputs` in their OpenRouter `supported_parameters`.
pub fn response_format(task: &str) -> Option<Value> {
    let verdict = || {
        json!({"type":"object","properties":{
            "verdict":{"type":"string","enum":["pass","needs_review","block"]},
            "reason_codes":{"type":"array","items":{"type":"string","enum":crate::sharing::CODES}}},
            "required":["verdict","reason_codes"],"additionalProperties":false})
    };
    Some(match task {
        "steer_screen" => schema(
            "steer_screen_verdict",
            json!({"type":"object","properties":{
                "allowed":{"type":"boolean"},
                "reason_code":{"type":"string","enum":with_empty(&STEER_REASONS)}},
                "required":["allowed","reason_code"],"additionalProperties":false}),
        ),
        "character_screen" => schema(
            "character_screen_verdict",
            json!({"type":"object","properties":{
                "approved":{"type":"boolean"},
                "reason_code":{"type":"string","enum":with_empty(&CHARACTER_REASONS)}},
                "required":["approved","reason_code"],"additionalProperties":false}),
        ),
        "course_screen" => schema("course_screen_verdict", verdict()),
        "screen_review" => schema("review_screen_verdict", verdict()),
        "course_brief_chat" => schema(
            "brief_classification",
            json!({"type":"object","properties":{"is_vague":{"type":"boolean"}},
                "required":["is_vague"],"additionalProperties":false}),
        ),
        "note" => schema(
            "note_enrichment",
            json!({"type":"object","properties":{
                "summary":{"type":"string"},
                "tags":{"type":"array","items":{"type":"string"}},
                "links":{"type":"array","items":{"type":"string"}}},
                "required":["summary","tags","links"],"additionalProperties":false}),
        ),
        "preview" => schema(
            "course_preview",
            json!({"type":"object","properties":{
                "title":{"type":"string"},
                "description":{"type":"string"},
                "units":{"type":"array","items":{"type":"object","properties":{
                    "title":{"type":"string"},
                    "lessons":{"type":"array","items":{"type":"object","properties":{
                        "title":{"type":"string"},"goal":{"type":"string"}},
                        "required":["title","goal"],"additionalProperties":false}}},
                    "required":["title","lessons"],"additionalProperties":false}},
                "slides":{"type":"array","items":{"type":"object","properties":{
                    "html":{"type":"string"},"narration":{"type":"string"},
                    "duration_ms":{"type":"integer"}},
                    "required":["html","narration","duration_ms"],"additionalProperties":false}}},
                "required":["title","description","units","slides"],"additionalProperties":false}),
        ),
        "lesson"
        | "published_copy_lesson"
        | "outline"
        | "outline_chat_change"
        | "outline_replan"
        | "character_create" => json!({"type":"json_object"}),
        _ => return None,
    })
}

/// Longest previous answer quoted back in a repair message.
pub const REPAIR_QUOTE_CHARS: usize = 600;

/// The corrective user turn for the one bounded repair: quotes the validation error and asks
/// for only the corrected JSON. Learner data is never in this text, only the rejected answer.
pub fn repair_message(error: &str) -> String {
    format!(
        "Your previous answer was rejected: {error}. Reply again with only one JSON object that \
         matches the required schema exactly: no prose, no Markdown fence, only the allowed enum \
         values."
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn unfenced_json_is_trimmed_only() {
        assert_eq!(extract_json("  {\"a\":1}\n"), "{\"a\":1}");
    }

    #[test]
    fn one_surrounding_fence_is_stripped() {
        assert_eq!(extract_json("```json\n{\"a\":1}\n```"), "{\"a\":1}");
        assert_eq!(extract_json("\n```\n{\"a\":1}\n```  \n"), "{\"a\":1}");
        assert_eq!(extract_json("```JSON\n[1]\n```"), "[1]");
    }

    #[test]
    fn prose_and_partial_answers_stay_unchanged() {
        for raw in [
            "**verdict:** pass",
            "pass",
            "allowed: true",
            "block phishing,personal_data",
            "Here is the JSON: {\"a\":1}",
            "```json\n{\"a\":1}",
            "{\"verdict\":\"pa",
        ] {
            assert_eq!(extract_json(raw), raw.trim(), "{raw}");
            assert!(serde_json::from_str::<Value>(extract_json(raw)).is_err());
        }
    }

    #[test]
    fn text_around_a_fence_or_two_fences_is_not_extracted() {
        let around = "Sure!\n```json\n{\"a\":1}\n```";
        assert_eq!(extract_json(around), around);
        let two = "```json\n{\"a\":1}\n```\n```json\n{\"b\":2}\n```";
        assert_eq!(extract_json(two), two);
        let tagged = "```json extra words\n{\"a\":1}\n```";
        assert_eq!(extract_json(tagged), tagged);
    }

    #[test]
    fn screens_get_strict_schemas_with_closed_enums() {
        for task in [
            "steer_screen",
            "character_screen",
            "course_screen",
            "screen_review",
        ] {
            let f = response_format(task).unwrap();
            assert_eq!(f["type"], "json_schema", "{task}");
            assert_eq!(f["json_schema"]["strict"], true);
            assert_eq!(f["json_schema"]["schema"]["additionalProperties"], false);
        }
        let review = response_format("screen_review").unwrap();
        let codes = &review["json_schema"]["schema"]["properties"]["reason_codes"]["items"]["enum"];
        assert!(codes.as_array().unwrap().contains(&json!("secret")));
        assert!(!codes.as_array().unwrap().contains(&json!("secrets")));
        assert!(!codes.as_array().unwrap().contains(&json!("phishing")));
        let steer = response_format("steer_screen").unwrap();
        assert_eq!(
            steer["json_schema"]["schema"]["properties"]["reason_code"]["enum"][0],
            ""
        );
    }

    #[test]
    fn generation_tasks_get_json_mode_and_prose_tasks_none() {
        assert_eq!(response_format("lesson").unwrap()["type"], "json_object");
        assert_eq!(response_format("outline").unwrap()["type"], "json_object");
        assert_eq!(response_format("preview").unwrap()["type"], "json_schema");
        assert!(response_format("chat").is_none());
        assert!(response_format("setup_chat").is_none());
    }

    #[test]
    fn repair_message_quotes_the_error() {
        let m = repair_message("reason_code 'secrets' is not one of secret, personal_data");
        assert!(m.contains("'secrets' is not one of"));
    }
}
