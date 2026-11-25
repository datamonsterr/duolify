//! Calibration fixtures. Mock results exercise the plumbing and are not promotion evidence.
use crate::rubric::{self, kappa};
use anyhow::{Context, Result, bail};
use serde_json::{Value, json};
use std::collections::BTreeMap;

#[derive(Clone, Copy)]
pub enum Degradation {
    WorkedExample,
    AbsurdDistractors,
    Summary,
    CopyNarration,
    LongSlide,
    Goal,
    Coverage,
    Pace,
    Hook,
    Feedback,
    Order,
}

pub fn degrade(lesson: &Value, kind: Degradation) -> Value {
    let mut copy = lesson.clone();
    match kind {
        Degradation::WorkedExample | Degradation::Summary | Degradation::Hook => {
            if let Some(slides) = copy["slides"].as_array_mut()
                && !slides.is_empty()
            {
                let i = match kind {
                    Degradation::WorkedExample => 2.min(slides.len() - 1),
                    Degradation::Hook => 0,
                    _ => slides.len() - 1,
                };
                slides.remove(i);
            }
        }
        Degradation::AbsurdDistractors => {
            for q in copy["test"].as_array_mut().into_iter().flatten() {
                let answer = q["answer_index"].as_u64().unwrap_or(0) as usize;
                if let Some(choices) = q["choices"].as_array_mut() {
                    for (i, c) in choices.iter_mut().enumerate() {
                        if i != answer {
                            *c = json!("A purple unicorn ate the moon");
                        }
                    }
                }
            }
        }
        Degradation::CopyNarration | Degradation::LongSlide => {
            if let Some(s) = copy["slides"].as_array_mut().and_then(|s| s.first_mut()) {
                if matches!(kind, Degradation::CopyNarration) {
                    s["narration"] =
                        json!(rubric::visible_text(s["html"].as_str().unwrap_or("")).trim());
                } else {
                    s["html"] = json!(format!("<p>{}</p>", vec!["word"; 80].join(" ")));
                }
            }
        }
        Degradation::Goal => copy["goal"] = json!("Understand the topic"),
        Degradation::Coverage => {
            if let Some(q) = copy["test"].as_array_mut().and_then(|q| q.first_mut()) {
                q["topic_id"] = json!("unrelated-topic");
            }
        }
        Degradation::Pace => {
            if let Some(s) = copy["slides"].as_array_mut().and_then(|s| s.first_mut()) {
                s["duration_ms"] = json!(1);
            }
        }
        Degradation::Feedback => {
            for q in copy["test"].as_array_mut().into_iter().flatten() {
                q["explanation"] = json!("");
            }
        }
        Degradation::Order => {
            copy["goal"] = json!("Requires untaught prerequisite before it is introduced")
        }
    }
    copy
}

#[derive(Clone)]
pub struct Anchor {
    pub criterion: String,
    pub expected: bool,
    pub input: Value,
    pub output: Value,
}

pub fn anchors() -> Result<Vec<Anchor>> {
    let files = [
        include_str!(
            "../../../../services/api/internal/examples/content/javascript-essentials-v1.json"
        ),
        include_str!(
            "../../../../services/api/internal/examples/content/product-management-foundations-v1.json"
        ),
    ];
    let mut anchors = Vec::new();
    for file in files {
        let course: Value = serde_json::from_str(file)?;
        let mut prior: Vec<Value> = Vec::new();
        for lesson in course["units"]
            .as_array()
            .into_iter()
            .flatten()
            .flat_map(|u| u["lessons"].as_array().into_iter().flatten())
        {
            let input = json!({"locale":"en", "goal":lesson["goal"], "previous_lessons":prior});
            for (criterion, degradation) in [
                ("R1", Degradation::Goal),
                ("R3", Degradation::Coverage),
                ("R4", Degradation::Hook),
                ("R5", Degradation::WorkedExample),
                ("R6", Degradation::Feedback),
                ("R7", Degradation::AbsurdDistractors),
                ("R9", Degradation::LongSlide),
                ("R10", Degradation::Pace),
            ] {
                // Some authored lessons exceed the R9 limits. They are useful sources for
                // degraded copies, but cannot honestly be labelled as R9 passes.
                let usable_pass = criterion != "R9"
                    || rubric::static_checks(&input, lesson).get("R9") == Some(&true);
                if usable_pass {
                    anchors.push(Anchor {
                        criterion: criterion.into(),
                        expected: true,
                        input: input.clone(),
                        output: lesson.clone(),
                    });
                }
                let damaged = degrade(lesson, degradation);
                let mut damaged_input = input.clone();
                if criterion == "R1" {
                    damaged_input["goal"] = damaged["goal"].clone();
                }
                anchors.push(Anchor {
                    criterion: criterion.into(),
                    expected: false,
                    input: damaged_input,
                    output: damaged,
                });
                if criterion == "R9" {
                    anchors.push(Anchor {
                        criterion: criterion.into(),
                        expected: false,
                        input: input.clone(),
                        output: degrade(lesson, Degradation::CopyNarration),
                    });
                }
            }
            anchors.push(Anchor {
                criterion: "R8".into(),
                expected: false,
                input: input.clone(),
                output: lesson.clone(),
            });
            if let Some(previous) = prior.last() {
                let ordered: Vec<Value> = prior
                    .iter()
                    .chain(std::iter::once(lesson))
                    .map(|l| json!({"title":l["title"], "goal":l["goal"]}))
                    .collect();
                let outline = json!({"units":[{"lessons": ordered}]});
                let mut broken = outline.clone();
                broken["units"][0]["lessons"][0]["goal"] = json!(format!(
                    "Use the untaught prerequisite {} before it is introduced",
                    lesson["title"].as_str().unwrap_or("")
                ));
                anchors.push(Anchor {
                    criterion: "R2".into(),
                    expected: true,
                    input: input.clone(),
                    output: outline,
                });
                anchors.push(Anchor {
                    criterion: "R2".into(),
                    expected: false,
                    input: input.clone(),
                    output: broken,
                });
                let mut reviewed = lesson.clone();
                let question = previous["test"]
                    .as_array()
                    .and_then(|q| q.first())
                    .context("earlier lesson has no question")?
                    .clone();
                reviewed["test"]
                    .as_array_mut()
                    .context("lesson has no questions")?
                    .push(question);
                anchors.push(Anchor {
                    criterion: "R8".into(),
                    expected: true,
                    input: input.clone(),
                    output: reviewed,
                });
            }
            prior.push(lesson.clone());
        }
    }
    Ok(anchors)
}

fn mock_judgment(anchor: &Anchor) -> bool {
    let lesson = &anchor.output;
    match anchor.criterion.as_str() {
        "R2" => !lesson.to_string().contains("untaught prerequisite"),
        "R4" => lesson["slides"]
            .as_array()
            .is_some_and(|s| s.iter().any(|slide| slide["slide_id"] == "s1")),
        "R5" => lesson["slides"]
            .as_array()
            .is_some_and(|s| s.iter().any(|slide| slide["slide_id"] == "s3")),
        "R6" => lesson["test"].as_array().is_some_and(|qs| {
            qs.iter()
                .all(|q| q["explanation"].as_str().is_some_and(|s| !s.is_empty()))
        }),
        "R7" => !lesson.to_string().contains("A purple unicorn ate the moon"),
        "R8" => {
            let old: Vec<&str> = anchor.input["previous_lessons"]
                .as_array()
                .into_iter()
                .flatten()
                .flat_map(|l| l["test"].as_array().into_iter().flatten())
                .filter_map(|q| q["question_id"].as_str())
                .collect();
            lesson["test"]
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(|q| q["question_id"].as_str())
                .any(|id| old.contains(&id))
        }
        _ => false,
    }
}

pub async fn run(mock: bool) -> Result<()> {
    let anchors = anchors()?;
    let router = if mock {
        None
    } else {
        Some(engine_providers::router("live")?)
    };
    let model = std::env::var("EVAL_JUDGE_MODEL").unwrap_or_else(|_| rubric::DEFAULT_JUDGE.into());
    if let Some(router) = &router {
        for route in ["lesson", "outline"] {
            let generator = router
                .models
                .tasks
                .get(route)
                .context("missing generator route")?;
            rubric::check_family(&model, &generator.model)?;
            for fallback in &generator.fallback_models {
                rubric::check_family(&model, fallback)?;
            }
        }
    }
    let mut pairs: BTreeMap<String, (Vec<bool>, Vec<bool>)> = BTreeMap::new();
    for anchor in &anchors {
        let actual = if ["R1", "R3", "R9", "R10"].contains(&anchor.criterion.as_str()) {
            *rubric::static_checks(&anchor.input, &anchor.output)
                .get(&anchor.criterion)
                .context("missing static check")?
        } else if let Some(router) = &router {
            let output = anchor.output.to_string();
            let route = engine_core::Route {
                model: model.clone(),
                ..Default::default()
            };
            let prompt = rubric::prompt(&anchor.criterion, &anchor.input, &output);
            let completion = router.primary.complete_route(&route, &prompt, 1000).await?;
            if completion.usage_unavailable {
                bail!("judge usage unavailable");
            }
            rubric::parse_verdict(&completion.text, &output)?
        } else {
            let output = anchor.output.to_string();
            let quote = rubric::mock_quote(&output)?;
            let verdict = if mock_judgment(anchor) {
                "PASS"
            } else {
                "FAIL"
            };
            let fixture = json!({"verdict":verdict, "evidence":quote}).to_string();
            rubric::parse_verdict(&fixture, &output)?
        };
        let entry = pairs.entry(anchor.criterion.clone()).or_default();
        entry.0.push(anchor.expected);
        entry.1.push(actual);
    }
    println!(
        "{} calibration (mock results are not promotion evidence)",
        if mock { "Mock" } else { "Live" }
    );
    let mut failed = false;
    for (criterion, (expected, actual)) in pairs {
        let score = kappa(&expected, &actual);
        println!(
            "{criterion}: kappa {} ({} anchors)",
            score.map_or_else(|| "undefined".into(), |k| format!("{k:.3}")),
            expected.len()
        );
        failed |= expected.len() < 20 || score.is_none_or(|k| k < 0.6);
    }
    if failed {
        bail!("judge calibration failed: kappa must be at least 0.6 on 20 anchors per criterion");
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn degraders_change_only_the_requested_elements() {
        let original = json!({"slides":[{"html":"<p>hook</p>","narration":"spoken"},{"html":"<p>idea</p>"},{"html":"<p>example</p>"},{"html":"<p>summary</p>"}],"test":[{"choices":["correct","wrong"],"answer_index":0}]});
        assert!(
            !degrade(&original, Degradation::WorkedExample)
                .to_string()
                .contains("example")
        );
        assert!(
            !degrade(&original, Degradation::Summary)
                .to_string()
                .contains("summary")
        );
        assert_eq!(
            degrade(&original, Degradation::CopyNarration)["slides"][0]["narration"],
            "hook"
        );
        assert_eq!(
            rubric::visible_text(
                degrade(&original, Degradation::LongSlide)["slides"][0]["html"]
                    .as_str()
                    .unwrap()
            )
            .split_whitespace()
            .count(),
            80
        );
        let damaged = degrade(&original, Degradation::AbsurdDistractors);
        assert_eq!(damaged["test"][0]["choices"][0], "correct");
        assert_ne!(damaged["test"][0]["choices"][1], "wrong");
        assert_eq!(original["slides"].as_array().unwrap().len(), 4);
    }
}
