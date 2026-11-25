//! Parity harness for the input guard (ADR 0025, task 5.3): the four screens (steer, character,
//! course publish, review) and the consolidated `input_guard` must agree on the same golden rows
//! before the API stops calling the four screens. Two steps, both offline:
//!
//! 1. `duolify-eval parity-rows <golden.jsonl> <out.jsonl>` turns every screen row into an
//!    `input_guard` row with the same text and the same `expected_blocked`.
//! 2. After the controller runs both row sets live (screens on the golden rows, the guard on the
//!    parity rows), `duolify-eval parity <screens-report.json> <guard-report.json>` joins the two
//!    reports by row and lists every disagreement.
//!
//! Gate: the guard misses no row the screens pass (a missed attack or a new false positive), and
//! both arms judged the same rows. Costs and latency are in the regular reports.
use anyhow::{Context, Result};
use serde_json::{Value, json};
use std::collections::BTreeMap;

const PREFIX: &str = "parity-";

/// The guard surface and text that stand in for one screen row, or `None` for a row the guard has
/// no equivalent of.
fn guard_input(row: &Value) -> Option<Value> {
    let input = &row["input"];
    let (surface, text, topic) = match row["task"].as_str()? {
        "steer_screen" => (
            "steer",
            input["text"].as_str()?.to_owned(),
            input["topic"].as_str().unwrap_or("Go").to_owned(),
        ),
        "character_screen" => (
            "character",
            input["prompt"]
                .as_str()
                .map(str::to_owned)
                .or_else(|| input.get("spec").map(Value::to_string))?,
            String::new(),
        ),
        "course_screen" => (
            "course_publish",
            input["sample"]
                .as_object()
                .map(|_| input["sample"].to_string())?,
            String::new(),
        ),
        "screen_review" => ("review", input["text"].as_str()?.to_owned(), String::new()),
        _ => return None,
    };
    Some(json!({
        "surface": surface,
        "text": text,
        "course_topic": topic,
        "expected_blocked": input["expected_blocked"],
    }))
}

/// Parity rows (JSON lines) for every screen row of `golden`.
pub fn parity_rows(golden: &str) -> Result<Vec<Value>> {
    let mut out = vec![];
    for line in golden.lines().filter(|l| !l.trim().is_empty()) {
        let row: Value = serde_json::from_str(line)?;
        let Some(input) = guard_input(&row) else {
            continue;
        };
        out.push(json!({
            "id": format!("{PREFIX}{}", row["id"].as_str().context("row without id")?),
            "task": "input_guard",
            "input": input,
            "rubric": "Parity with the four screens: the guard reaches the same verdict as the screen row it mirrors.",
            "checks": [
                {"kind": "path_equals", "critical": true, "value": "{\"path\": \"blocked_as_expected\", \"expected\": true}"},
                {"kind": "path_equals", "critical": true, "value": "{\"path\": \"unavailable\", \"expected\": false}"},
            ],
        }));
    }
    Ok(out)
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Disagreement {
    pub id: String,
    pub screens_passed: bool,
    pub guard_passed: bool,
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct Parity {
    pub paired: usize,
    pub agree: usize,
    pub disagreements: Vec<Disagreement>,
    /// Parity rows without a screen row in the report (or the reverse): the arms judged different rows.
    pub unmatched: Vec<String>,
}

impl Parity {
    /// True when the guard may replace the screens: same rows on both sides and no row the screens
    /// pass that the guard fails. A row only the guard passes is reported but does not block.
    pub fn ok(&self) -> bool {
        self.unmatched.is_empty()
            && self.paired > 0
            && self
                .disagreements
                .iter()
                .all(|d| d.guard_passed && !d.screens_passed)
    }
}

fn passes(report: &Value) -> Result<BTreeMap<String, bool>> {
    Ok(report["results"]
        .as_array()
        .context("report without results")?
        .iter()
        .filter_map(|r| Some((r["id"].as_str()?.to_owned(), r["passed"].as_bool()?)))
        .collect())
}

/// Joins the screens report and the guard report by row id (`parity-<id>`).
pub fn compare(screens: &Value, guard: &Value) -> Result<Parity> {
    let screens = passes(screens)?;
    let guard: BTreeMap<String, bool> = passes(guard)?
        .into_iter()
        .filter_map(|(id, p)| Some((id.strip_prefix(PREFIX)?.to_owned(), p)))
        .collect();
    let mut p = Parity::default();
    for (id, guard_passed) in &guard {
        match screens.get(id) {
            None => p.unmatched.push(format!("{PREFIX}{id}")),
            Some(screens_passed) => {
                p.paired += 1;
                if screens_passed == guard_passed {
                    p.agree += 1;
                } else {
                    p.disagreements.push(Disagreement {
                        id: id.clone(),
                        screens_passed: *screens_passed,
                        guard_passed: *guard_passed,
                    });
                }
            }
        }
    }
    Ok(p)
}

pub fn render(p: &Parity) -> String {
    let mut s = format!(
        "parity: {} paired, {} agree, {} disagree, {} unmatched\n",
        p.paired,
        p.agree,
        p.disagreements.len(),
        p.unmatched.len()
    );
    for d in &p.disagreements {
        let kind = if d.screens_passed {
            "GUARD REGRESSION (screens pass, guard fails)"
        } else {
            "guard better (screens fail, guard passes)"
        };
        s.push_str(&format!("  {}: {kind}\n", d.id));
    }
    for u in &p.unmatched {
        s.push_str(&format!("  {u}: no screen row in the report\n"));
    }
    s.push_str(if p.ok() {
        "verdict: PARITY (the guard may replace the screens)\n"
    } else {
        "verdict: NO PARITY (keep the four screens)\n"
    });
    s
}

/// `duolify-eval parity-rows <golden.jsonl> <out.jsonl>`
pub fn run_rows(args: &[String]) -> Result<()> {
    let (src, dst) = (
        args.get(2)
            .context("usage: duolify-eval parity-rows <golden.jsonl> <out.jsonl>")?,
        args.get(3)
            .context("usage: duolify-eval parity-rows <golden.jsonl> <out.jsonl>")?,
    );
    let rows = parity_rows(&std::fs::read_to_string(src)?)?;
    let body: String = rows.iter().map(|r| format!("{r}\n")).collect();
    std::fs::write(dst, body)?;
    println!("wrote {} parity rows to {dst}", rows.len());
    Ok(())
}

/// `duolify-eval parity <screens-report.json> <guard-report.json>`; fails when there is no parity.
pub fn run(args: &[String]) -> Result<()> {
    let read = |i: usize| -> Result<Value> {
        let p = args
            .get(i)
            .context("usage: duolify-eval parity <screens-report.json> <guard-report.json>")?;
        Ok(serde_json::from_str(&std::fs::read_to_string(p)?)?)
    };
    let p = compare(&read(2)?, &read(3)?)?;
    print!("{}", render(&p));
    anyhow::ensure!(p.ok(), "no parity");
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn report(rows: &[(&str, bool)]) -> Value {
        json!({"results": rows.iter().map(|(id, p)| json!({"id": id, "passed": p})).collect::<Vec<_>>()})
    }

    #[test]
    fn every_screen_row_of_the_golden_set_has_a_guard_row_with_the_same_expectation() {
        let rows = parity_rows(include_str!(
            "../../../../evals/datasets/duolify-golden/golden.jsonl"
        ))
        .unwrap();
        assert_eq!(rows.len(), 9, "3 steer, 2 character, 2 course, 2 review");
        let surfaces: Vec<&str> = rows
            .iter()
            .map(|r| r["input"]["surface"].as_str().unwrap())
            .collect();
        for s in ["steer", "character", "course_publish", "review"] {
            assert!(surfaces.contains(&s), "{s}");
        }
        assert!(
            rows.iter()
                .all(|r| r["input"]["expected_blocked"].is_boolean())
        );
        assert!(
            rows.iter()
                .all(|r| r["id"].as_str().unwrap().starts_with("parity-live-"))
        );
    }

    #[test]
    fn identical_outcomes_are_parity_and_a_regression_is_not() {
        let screens = report(&[("a", true), ("b", true), ("c", false)]);
        let same = report(&[("parity-a", true), ("parity-b", true), ("parity-c", false)]);
        let p = compare(&screens, &same).unwrap();
        assert!(p.ok() && p.agree == 3 && p.paired == 3);
        let regressed = report(&[("parity-a", true), ("parity-b", false), ("parity-c", true)]);
        let p = compare(&screens, &regressed).unwrap();
        assert!(!p.ok());
        assert_eq!(p.disagreements.len(), 2);
        assert!(render(&p).contains("GUARD REGRESSION"));
        // A guard that only fixes a row the screens fail is reported, not blocked.
        let better = report(&[("parity-a", true), ("parity-b", true), ("parity-c", true)]);
        let p = compare(&screens, &better).unwrap();
        assert!(p.ok() && p.disagreements.len() == 1);
    }

    #[test]
    fn missing_rows_on_either_side_are_no_parity() {
        let screens = report(&[("a", true)]);
        let guard = report(&[("parity-a", true), ("parity-zzz", true)]);
        let p = compare(&screens, &guard).unwrap();
        assert_eq!(p.unmatched, ["parity-zzz"]);
        assert!(!p.ok());
        assert!(!compare(&screens, &report(&[("other", true)])).unwrap().ok());
    }
}
