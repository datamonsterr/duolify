use crate::scoring::EvalReport;
use anyhow::Result;

pub fn generate_json_report(report: &EvalReport) -> Result<Vec<u8>> {
    Ok(serde_json::to_vec_pretty(report)?)
}

pub fn generate_markdown_report(report: &EvalReport) -> String {
    let mut md = String::new();
    md.push_str("# Eval Report\n\n");
    md.push_str(&format!(
        "- **Overall Result**: {}\n",
        if report.passed { "PASSED" } else { "FAILED" }
    ));
    md.push_str(&format!("- **Status**: {}\n", report.status));
    md.push_str(&format!(
        "- **Critical Check Failure**: {}\n",
        if report.critical_failure { "YES" } else { "NO" }
    ));
    md.push_str(&format!(
        "- **Budget Exceeded**: {}\n",
        if report.budget_exceeded { "YES" } else { "NO" }
    ));
    md.push_str(&format!("- **Provider**: {}\n", report.provider));
    if let Some(b) = report.budget_usd {
        md.push_str(&format!("- **Budget (USD)**: ${:.2}\n", b));
    }
    md.push_str(&format!(
        "- **Total Cost (USD)**: ${:.4}\n",
        report.total_cost_usd
    ));
    md.push_str(&format!(
        "- **Live Promoted**: {}\n",
        if report.live_promoted { "YES" } else { "NO" }
    ));
    md.push_str(&format!("- **Dataset**: {}\n", report.dataset));
    md.push_str(&format!(
        "- **Dataset Revision**: {}\n",
        report.dataset_revision
    ));
    md.push_str(&format!(
        "- **Dataset Source**: {}\n",
        report.dataset_source
    ));
    md.push_str(&format!(
        "- **Prompt Revision**: {}\n\n",
        report.prompt_revision
    ));

    md.push_str("## Tasks\n\n");
    md.push_str("| Task | Model | Score | Threshold | Passed | Critical Failure | Cases |\n");
    md.push_str("|---|---|---|---|---|---|---|\n");

    for (task, tr) in &report.task_reports {
        md.push_str(&format!(
            "| {} | {} | {:.2} | {:.2} | {} | {} | {}/{} |\n",
            task,
            tr.model,
            tr.score,
            tr.threshold,
            if tr.passed { "PASS" } else { "FAIL" },
            if tr.critical_failure { "YES" } else { "NO" },
            tr.passed_cases,
            tr.total_cases,
        ));
    }

    let failing_cases: Vec<_> = report.results.iter().filter(|r| !r.passed).collect();
    if !failing_cases.is_empty() {
        md.push_str("\n## Failing Cases\n\n");
        for case in failing_cases {
            md.push_str(&format!(
                "- **{}** (task: {}): critical_fail={}, errors: {}\n",
                case.id,
                case.task,
                case.critical_failed,
                case.errors.join(", ")
            ));
        }
    }

    md
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::scoring::{CaseResult, TaskReport};
    use std::collections::BTreeMap;

    #[test]
    fn test_report_formatting() {
        let mut task_reports = BTreeMap::new();
        task_reports.insert(
            "course_preview".into(),
            TaskReport {
                task: "course_preview".into(),
                model: "claude-sonnet-5-5".into(),
                score: 1.0,
                threshold: 1.0,
                passed: true,
                critical_failure: false,
                total_cases: 5,
                passed_cases: 5,
                deterministic_score: 1.0,
                judge_score: Some(1.0),
            },
        );

        let report = EvalReport {
            timestamp_millis: 1000,
            dataset: "datamonsterr/duolify-golden".into(),
            dataset_revision: "rev123".into(),
            dataset_source: "mirror".into(),
            prompt_revision: "git-abc".into(),
            provider: "mock".into(),
            budget_usd: Some(5.0),
            total_cost_usd: 0.0,
            budget_exceeded: false,
            status: "passed".into(),
            live_promoted: false,
            passed: true,
            critical_failure: false,
            task_reports,
            results: vec![CaseResult {
                id: "case-1".into(),
                task: "course_preview".into(),
                passed: true,
                critical_failed: false,
                deterministic_passed: true,
                judge_passed: Some(true),
                errors: vec![],
                cost_usd: 0.0,
                input_tokens: 0,
                output_tokens: 0,
            }],
        };

        let json_bytes = generate_json_report(&report).unwrap();
        assert!(!json_bytes.is_empty());

        let md = generate_markdown_report(&report);
        assert!(md.contains("# Eval Report"));
        assert!(md.contains("PASSED"));
        assert!(md.contains("datamonsterr/duolify-golden"));
        assert!(md.contains("rev123"));
        assert!(md.contains("git-abc"));
        assert!(md.contains("course_preview"));
    }
}
