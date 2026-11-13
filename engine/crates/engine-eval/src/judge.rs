use anyhow::{Result, bail};
use regex::Regex;
use std::sync::LazyLock;

static PASS_RE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"(?i)\bPASS\b").unwrap());
static FAIL_RE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"(?i)\bFAIL\b").unwrap());

pub fn parse_judge_verdict(raw: &str) -> Result<bool> {
    let trimmed = raw.trim();

    // 1. Check JSON structured verdict
    let json_val = serde_json::from_str::<serde_json::Value>(trimmed)
        .ok()
        .or_else(|| {
            if let Some(start) = trimmed.find('{')
                && let Some(end) = trimmed.rfind('}')
                && start < end
            {
                serde_json::from_str::<serde_json::Value>(&trimmed[start..=end]).ok()
            } else {
                None
            }
        });

    if let Some(value) = json_val {
        if let Some(passed) = value.get("passed").and_then(|v| v.as_bool()) {
            return Ok(passed);
        }
        if let Some(verdict) = value.get("verdict").and_then(|v| v.as_str()) {
            let v = verdict.trim();
            let has_pass = PASS_RE.is_match(v);
            let has_fail = FAIL_RE.is_match(v);
            if has_pass && !has_fail {
                return Ok(true);
            }
            if has_fail && !has_pass {
                return Ok(false);
            }
        }
    }

    // 2. Check last non-empty line using strict word boundaries
    if let Some(last_line) = trimmed.lines().rev().find(|l| !l.trim().is_empty()) {
        let last = last_line.trim();
        let has_pass = PASS_RE.is_match(last);
        let has_fail = FAIL_RE.is_match(last);

        if has_pass && !has_fail {
            return Ok(true);
        }
        if has_fail && !has_pass {
            return Ok(false);
        }
    }

    bail!("unable to parse judge verdict from: {}", raw)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_verdict_parsing() {
        assert!(parse_judge_verdict("PASS").unwrap());
        assert!(parse_judge_verdict("pass").unwrap());
        assert!(!parse_judge_verdict("FAIL").unwrap());
        assert!(!parse_judge_verdict("fail").unwrap());
        assert!(parse_judge_verdict("Verdict: PASS").unwrap());
        assert!(!parse_judge_verdict("Verdict: FAIL").unwrap());
        assert!(parse_judge_verdict("The rubric was fulfilled.\n\nPASS").unwrap());
        assert!(parse_judge_verdict(r#"{"verdict": "PASS"}"#).unwrap());
        assert!(!parse_judge_verdict(r#"{"passed": false}"#).unwrap());
    }

    #[test]
    fn test_verdict_rejects_substring_pass_and_requires_word_boundaries() {
        assert!(parse_judge_verdict("This explains the passage of time.").is_err());
        assert!(parse_judge_verdict("We must bypass the restriction.").is_err());
        assert!(parse_judge_verdict("Compassionate feedback for the learner.").is_err());
        assert!(
            parse_judge_verdict("Some text with PASS in line 1\nbut line 2 is general discussion")
                .is_err()
        );
    }
}
