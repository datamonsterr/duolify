//! Arm comparison for prompt routing (ADR 0025, task 7.1): two live reports of the same cases, one
//! per `PROMPT_ARM`, paired by case id. Output is per-arm tokens, cost, latency p50/p95 and pass
//! rate, the paired cost difference with its standard error (the noise margin) and the economics
//! gate verdict: routed passes at least as often and costs at most 3 percent more.
use anyhow::{Context, Result};
use serde_json::Value;
use std::collections::BTreeMap;

/// Largest cost increase of the candidate arm that still passes the gate.
pub const MAX_COST_INCREASE: f64 = 0.03;

#[derive(Debug, Clone, PartialEq)]
pub struct Arm {
    pub n: usize,
    pub pass_rate: f64,
    pub cost_usd: f64,
    pub mean_input_tokens: f64,
    pub mean_output_tokens: f64,
    pub latency_p50_ms: f64,
    pub latency_p95_ms: f64,
}

#[derive(Debug, Clone, PartialEq)]
pub struct Comparison {
    pub baseline: Arm,
    pub candidate: Arm,
    pub paired: usize,
    /// Mean of (candidate cost - baseline cost) over paired cases, and its standard error.
    pub mean_cost_diff_usd: f64,
    pub cost_diff_stderr_usd: f64,
    pub cost_increase: f64,
    pub promote: bool,
}

type Row = (bool, f64, f64, f64, f64);

fn rows(report: &Value) -> Result<BTreeMap<String, Row>> {
    let results = report["results"]
        .as_array()
        .context("report without results")?;
    Ok(results
        .iter()
        .filter_map(|r| {
            Some((
                r["id"].as_str()?.to_owned(),
                (
                    r["passed"].as_bool()?,
                    r["cost_usd"].as_f64().unwrap_or(0.0),
                    r["input_tokens"].as_f64().unwrap_or(0.0),
                    r["output_tokens"].as_f64().unwrap_or(0.0),
                    r["latency_ms"].as_f64().unwrap_or(0.0),
                ),
            ))
        })
        .collect())
}

fn percentile(sorted: &[f64], p: f64) -> f64 {
    if sorted.is_empty() {
        return 0.0;
    }
    let rank = (p * sorted.len() as f64).ceil() as usize;
    sorted[rank.clamp(1, sorted.len()) - 1]
}

fn arm(rows: &[&Row]) -> Arm {
    let n = rows.len();
    let f = n.max(1) as f64;
    let mut lat: Vec<f64> = rows.iter().map(|r| r.4).collect();
    lat.sort_by(f64::total_cmp);
    Arm {
        n,
        pass_rate: rows.iter().filter(|r| r.0).count() as f64 / f,
        cost_usd: rows.iter().map(|r| r.1).sum(),
        mean_input_tokens: rows.iter().map(|r| r.2).sum::<f64>() / f,
        mean_output_tokens: rows.iter().map(|r| r.3).sum::<f64>() / f,
        latency_p50_ms: percentile(&lat, 0.50),
        latency_p95_ms: percentile(&lat, 0.95),
    }
}

/// Compares `candidate` against `baseline` on the cases both ran.
pub fn compare(baseline: &Value, candidate: &Value) -> Result<Comparison> {
    let (b, c) = (rows(baseline)?, rows(candidate)?);
    let ids: Vec<&String> = b.keys().filter(|k| c.contains_key(*k)).collect();
    anyhow::ensure!(!ids.is_empty(), "the reports share no case ids");
    let bs: Vec<&Row> = ids.iter().map(|k| &b[*k]).collect();
    let cs: Vec<&Row> = ids.iter().map(|k| &c[*k]).collect();
    let (ba, ca) = (arm(&bs), arm(&cs));
    let diffs: Vec<f64> = bs.iter().zip(&cs).map(|(x, y)| y.1 - x.1).collect();
    let n = diffs.len() as f64;
    let mean = diffs.iter().sum::<f64>() / n;
    let var = if diffs.len() > 1 {
        diffs.iter().map(|d| (d - mean).powi(2)).sum::<f64>() / (n - 1.0)
    } else {
        0.0
    };
    let increase = if ba.cost_usd > 0.0 {
        (ca.cost_usd - ba.cost_usd) / ba.cost_usd
    } else {
        0.0
    };
    Ok(Comparison {
        promote: ca.pass_rate >= ba.pass_rate && increase <= MAX_COST_INCREASE,
        baseline: ba,
        candidate: ca,
        paired: ids.len(),
        mean_cost_diff_usd: mean,
        cost_diff_stderr_usd: (var / n).sqrt(),
        cost_increase: increase,
    })
}

pub fn render(c: &Comparison) -> String {
    let line = |name: &str, a: &Arm| {
        format!(
            "{name:<10} n={} pass={:.3} cost=${:.6} in_tok={:.0} out_tok={:.0} p50={:.0}ms p95={:.0}ms\n",
            a.n,
            a.pass_rate,
            a.cost_usd,
            a.mean_input_tokens,
            a.mean_output_tokens,
            a.latency_p50_ms,
            a.latency_p95_ms
        )
    };
    format!(
        "{}{}paired={} cost diff/case={:+.8} USD (stderr {:.8}) cost increase={:+.2}%\ngate (pass rate not lower, cost increase <= {:.0}%): {}\n",
        line("baseline", &c.baseline),
        line("candidate", &c.candidate),
        c.paired,
        c.mean_cost_diff_usd,
        c.cost_diff_stderr_usd,
        c.cost_increase * 100.0,
        MAX_COST_INCREASE * 100.0,
        if c.promote { "PROMOTE" } else { "HOLD" }
    )
}

/// `duolify-eval compare <baseline.json> <candidate.json>`.
pub fn run(args: &[String]) -> Result<()> {
    let read = |i: usize| -> Result<Value> {
        let p = args
            .get(i)
            .context("usage: duolify-eval compare <baseline.json> <candidate.json>")?;
        Ok(serde_json::from_str(&std::fs::read_to_string(p)?)?)
    };
    let c = compare(&read(2)?, &read(3)?)?;
    print!("{}", render(&c));
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn report(rows: &[(&str, bool, f64, f64)]) -> Value {
        json!({"results": rows.iter().map(|(id, p, cost, lat)| json!({
            "id": id, "passed": p, "cost_usd": cost, "input_tokens": 100, "output_tokens": 50, "latency_ms": lat
        })).collect::<Vec<_>>()})
    }

    #[test]
    fn promotes_when_not_worse_and_within_three_percent() {
        let b = report(&[("a", true, 1.0, 100.0), ("b", false, 1.0, 200.0)]);
        let c = report(&[
            ("a", true, 1.02, 90.0),
            ("b", true, 1.0, 180.0),
            ("only-c", true, 9.0, 1.0),
        ]);
        let r = compare(&b, &c).unwrap();
        assert_eq!(r.paired, 2, "unpaired cases are ignored");
        assert!(r.promote);
        assert!((r.cost_increase - 0.01).abs() < 1e-9);
        assert_eq!(r.candidate.pass_rate, 1.0);
        assert_eq!(r.baseline.latency_p95_ms, 200.0);
    }

    #[test]
    fn holds_on_a_lower_pass_rate_or_a_cost_increase_over_three_percent() {
        let b = report(&[("a", true, 1.0, 1.0), ("b", true, 1.0, 1.0)]);
        let worse = report(&[("a", true, 1.0, 1.0), ("b", false, 1.0, 1.0)]);
        assert!(!compare(&b, &worse).unwrap().promote);
        let dearer = report(&[("a", true, 1.05, 1.0), ("b", true, 1.05, 1.0)]);
        let r = compare(&b, &dearer).unwrap();
        assert!(!r.promote);
        assert!(
            r.cost_diff_stderr_usd.abs() < 1e-12,
            "equal diffs have no spread"
        );
        assert!(render(&r).contains("HOLD"));
    }

    #[test]
    fn reports_without_shared_cases_are_an_error() {
        assert!(
            compare(
                &report(&[("a", true, 1.0, 1.0)]),
                &report(&[("b", true, 1.0, 1.0)])
            )
            .is_err()
        );
    }
}
