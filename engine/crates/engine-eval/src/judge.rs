use anyhow::{Result, bail};

pub fn parse_judge_verdict(raw: &str) -> Result<bool> {
    let trimmed = raw.trim();

    // Check JSON structured verdict
    if let Ok(value) = serde_json::from_str::<serde_json::Value>(trimmed) {
        if let Some(passed) = value.get("passed").and_then(|v| v.as_bool()) {
            return Ok(passed);
        }
        if let Some(verdict) = value.get("verdict").and_then(|v| v.as_str()) {
            let v = verdict.trim();
            if v.eq_ignore_ascii_case("PASS") {
                return Ok(true);
            } else if v.eq_ignore_ascii_case("FAIL") {
                return Ok(false);
            }
        }
    }

    // Direct match
    if trimmed.eq_ignore_ascii_case("PASS") {
        return Ok(true);
    }
    if trimmed.eq_ignore_ascii_case("FAIL") {
        return Ok(false);
    }

    // Check last non-empty line
    if let Some(last_line) = trimmed.lines().rev().find(|l| !l.trim().is_empty()) {
        let last = last_line.trim();
        if last.eq_ignore_ascii_case("PASS") || last.eq_ignore_ascii_case("VERDICT: PASS") {
            return Ok(true);
        }
        if last.eq_ignore_ascii_case("FAIL") || last.eq_ignore_ascii_case("VERDICT: FAIL") {
            return Ok(false);
        }
    }

    // Keyword detection
    let upper = trimmed.to_uppercase();
    let has_pass = upper.contains("PASS");
    let has_fail = upper.contains("FAIL");

    if has_pass && !has_fail {
        return Ok(true);
    }
    if has_fail && !has_pass {
        return Ok(false);
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
}
