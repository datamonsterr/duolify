//! Live golden cases for the non-screen tasks. Each one runs the production prompt builder,
//! validator and bounded runner against the live router, so the eval tests what production sends.
//! The case `input` holds only the request data the API would pass to the Engine.
use anyhow::{Context, Result};
use engine_core::{
    Completion, ProviderRouter,
    credits::{Attempt, Limits},
};
use futures::StreamExt;
use serde_json::{Value, json};
use std::sync::Mutex;

/// One live case: the scored JSON text plus every billed attempt.
pub struct Live {
    pub text: String,
    pub attempts: Vec<Attempt>,
    pub completion: Option<Completion>,
}

/// Tasks whose live cases go through production builders here (screens run in the runner).
pub const TASKS: [&str; 8] = [
    "course_preview",
    "course_outline",
    "lesson_slides",
    "lesson_test",
    "tutor_chat",
    "note_enrich",
    "setup_chat",
    "course_brief_chat",
];

fn limits(input: &Value, input_tokens: u32, output_tokens: u32, timeout_ms: u32) -> Limits {
    input
        .get("limits")
        .and_then(|l| serde_json::from_value(l.clone()).ok())
        .unwrap_or(Limits {
            input_tokens,
            output_tokens,
            timeout_ms,
            max_attempts: 2,
            max_cost_nanos: 1_000_000_000,
            input_nanos: 1000,
            output_nanos: 5000,
            cache_write_nanos: 1250,
            cache_read_nanos: 100,
            deadline_unix_ms: 0,
        })
}

fn bounded(outcome: engine_core::credits::Outcome, output: Value) -> Live {
    let mut value = json!({
        "delivered": outcome.completion.is_some(),
        "attempt_count": outcome.attempts.len(),
        "fallback_used": outcome.attempts.iter().any(|a| a.fallback_used),
    });
    if let (Some(o), Some(map)) = (output.as_object(), value.as_object_mut()) {
        map.extend(o.clone());
    }
    Live {
        text: value.to_string(),
        attempts: outcome.attempts,
        completion: outcome.completion,
    }
}

/// Wraps a production validator and keeps each rejection reason with an excerpt of the answer,
/// so a failing live case shows why. The verdict itself is the production one.
struct Rejections(Mutex<Vec<String>>);
impl Rejections {
    fn new() -> Self {
        Self(Mutex::new(Vec::new()))
    }
    fn check(
        &self,
        text: &str,
        verdict: std::result::Result<(), String>,
    ) -> std::result::Result<(), String> {
        if let Err(e) = &verdict {
            let excerpt: String = text.chars().take(160).collect();
            self.0.lock().unwrap().push(format!("{e} | {excerpt}"));
        }
        verdict
    }
    fn into_value(self) -> Value {
        json!(self.0.into_inner().unwrap())
    }
}

fn parsed(c: &Option<Completion>) -> Value {
    c.as_ref()
        .and_then(|c| serde_json::from_str(&c.text).ok())
        .unwrap_or(Value::Null)
}

pub async fn evaluate(task: &str, input: &Value, router: &ProviderRouter) -> Result<Live> {
    match task {
        "course_preview" => {
            let brief: engine_core::courses::Brief =
                serde_json::from_value(input["brief"].clone())?;
            let prompt = engine_core::courses::preview_prompt(&brief, input["profile"].clone());
            let rejected = Rejections::new();
            let outcome = engine_core::credits::run_validated_checked(
                router,
                "preview",
                &prompt,
                &limits(input, 16000, 8000, 120000),
                |text| rejected.check(text, engine_core::courses::check_preview(text)),
            )
            .await?;
            let preview = parsed(&outcome.completion);
            let rejected = rejected.into_value();
            Ok(bounded(
                outcome,
                json!({
                    "rejected": rejected,
                    "title": preview["title"],
                    "unit_count": preview["units"].as_array().map_or(0, Vec::len),
                    "slide_count": preview["slides"].as_array().map_or(0, Vec::len),
                }),
            ))
        }
        "course_outline" => {
            use engine_core::courses::outline;
            let base: Vec<outline::Unit> = serde_json::from_value(input["outline"].clone())?;
            let prompt =
                outline::creation_prompt(&base, input["brief"].clone(), input["profile"].clone())?;
            let outcome = engine_core::credits::run_validated(
                router,
                "outline",
                &prompt,
                &limits(input, 12000, 4000, 60000),
                |text| outline::validate_created(&base, text),
            )
            .await?;
            let units = parsed(&outcome.completion)["units"].clone();
            let adhered = serde_json::from_value::<Vec<outline::Unit>>(units.clone())
                .is_ok_and(|u| u == base);
            Ok(bounded(
                outcome,
                json!({"adhered": adhered, "units": units}),
            ))
        }
        "lesson_slides" => {
            use engine_proto as pb;
            // The same request data the API sends; Debug-rendered exactly like the server does.
            let context = pb::GenerateLessonRequest {
                outline: Some(serde_json::from_value::<Outline>(input["outline"].clone())?.into()),
                lesson: Some(pb::LessonRef {
                    lesson_id: input["lesson"]["id"].as_str().unwrap_or_default().into(),
                    title: input["lesson"]["title"].as_str().unwrap_or_default().into(),
                    goal: input["lesson"]["goal"].as_str().unwrap_or_default().into(),
                }),
                learner: Some(pb::LearnerProfile {
                    locale: input["learner"]["locale"]
                        .as_str()
                        .unwrap_or_default()
                        .into(),
                    level: input["learner"]["level"]
                        .as_str()
                        .unwrap_or_default()
                        .into(),
                    goals: input["learner"]["goals"]
                        .as_str()
                        .unwrap_or_default()
                        .into(),
                    ..Default::default()
                }),
                history: serde_json::from_value(input["history"].clone()).unwrap_or_default(),
                ..Default::default()
            };
            let prompt = engine_core::steering::hardened_prompt(
                &engine_core::lessontest::lesson_prompt(&format!("{context:?}")),
                &[],
            )?;
            let rejected = Rejections::new();
            let outcome = engine_core::eval::run_bounded_formatted(
                router,
                "lesson",
                &[&prompt],
                &limits(input, 16000, 12000, 180000),
                engine_core::eval::Shape {
                    format: Some(engine_core::structured::lesson_format()),
                    prepare: Some(engine_core::lessontest::prepare_lesson),
                },
                |text| {
                    rejected.check(
                        text,
                        engine_core::lessontest::check_generated(text, &[], ""),
                    )
                },
                true,
            )
            .await?;
            let lesson = parsed(&outcome.completion);
            Ok(bounded(
                outcome,
                json!({
                    "rejected": rejected.into_value(),
                    "slide_count": lesson["slides"].as_array().map_or(0, Vec::len),
                    "test_count": lesson["test"].as_array().map_or(0, Vec::len),
                    "title": lesson["title"],
                }),
            ))
        }
        "lesson_test" => {
            use engine_core::lessontest::retry;
            let source: retry::Question = serde_json::from_value(input["source_question"].clone())?;
            let previous: Vec<retry::Question> =
                serde_json::from_value(input["previous_variants"].clone()).unwrap_or_default();
            let outcome = retry::generate(
                router,
                &source,
                &previous,
                &limits(input, 8000, 2000, 60000),
            )
            .await?;
            let variants = parsed(&outcome.completion)["variants"].clone();
            Ok(bounded(outcome, json!({"variants": variants})))
        }
        "note_enrich" => {
            use engine_core::notes;
            let lesson: Option<notes::LessonRef> =
                serde_json::from_value(input["lesson"].clone()).ok();
            let learner: Option<notes::LearnerProfile> =
                serde_json::from_value(input["learner"].clone()).ok();
            let allowed: Vec<notes::LessonRef> =
                serde_json::from_value(input["allowed_lessons"].clone()).unwrap_or_default();
            let out = notes::enrich(
                router,
                input["note"].as_str().unwrap_or_default(),
                lesson.as_ref(),
                learner.as_ref(),
                &allowed,
                &limits(input, 8000, 1000, 30000),
            )
            .await?;
            let result = serde_json::to_value(&out.result)?;
            Ok(bounded(
                engine_core::credits::Outcome {
                    completion: out.completion,
                    attempts: out.attempts,
                },
                json!({"result": result}),
            ))
        }
        "course_brief_chat" => {
            use engine_core::courses::{Brief, Limits as BriefLimits, process};
            let brief: Brief = serde_json::from_value(input["brief"].clone()).unwrap_or_default();
            let outcome = process(
                router,
                brief,
                input["step"].as_str().context("step")?,
                input["answer"].as_str().unwrap_or_default(),
                false,
                input["is_follow_up"].as_bool().unwrap_or(false),
                &BriefLimits {
                    input_tokens: 2000,
                    output_tokens: 1000,
                    attempt_timeout_ms: 3000,
                    max_attempts: 3,
                    max_cost_nanos: 2_100_000,
                    input_nanos_per_token: 100,
                    output_nanos_per_token: 500,
                },
            )
            .await?;
            let attempts: Vec<Attempt> = outcome
                .attempts
                .iter()
                .enumerate()
                .map(|(i, a)| attempt(a.completion.as_ref(), &a.model, i, a.fallback, a.latency_ms))
                .collect();
            let completion = outcome
                .attempts
                .iter()
                .rev()
                .find_map(|a| a.completion.clone());
            let mut value = match outcome.result {
                Some(r) => serde_json::to_value(r)?,
                None => json!({"form_fallback": true}),
            };
            value["attempt_count"] = json!(attempts.len());
            Ok(Live {
                text: value.to_string(),
                attempts,
                completion,
            })
        }
        "setup_chat" => {
            use engine_core::accounts;
            struct Adapter<'a> {
                router: &'a ProviderRouter,
                seen: Mutex<Vec<Completion>>,
            }
            #[async_trait::async_trait]
            impl accounts::ModelProvider for Adapter<'_> {
                async fn complete(&self, prompt: &str) -> Result<String> {
                    // The production adapter (engine-server `setup_chat`).
                    let c = self.router.complete("setup_chat", prompt).await?;
                    let text = c.text.clone();
                    self.seen.lock().unwrap().push(c);
                    Ok(text)
                }
            }
            let adapter = Adapter {
                router,
                seen: Mutex::new(Vec::new()),
            };
            let mut state = accounts::SetupState::default();
            let turn = accounts::SetupChatTurn {
                step: accounts::SetupStep::parse(input["step"].as_str().unwrap_or("goal")),
                answer: input["answer"].as_str().unwrap_or_default().into(),
                skipped: false,
                is_follow_up: input["is_follow_up"].as_bool().unwrap_or(false),
            };
            let result = accounts::process_setup_chat(&mut state, &turn, &adapter).await;
            let seen = adapter.seen.into_inner().unwrap();
            let attempts = seen
                .iter()
                .enumerate()
                .map(|(i, c)| attempt(Some(c), &c.model, i, false, 0))
                .collect();
            let text = match result {
                Ok(r) => serde_json::to_value(r)?,
                Err(_) => json!({"unavailable": true}),
            };
            Ok(Live {
                text: text.to_string(),
                attempts,
                completion: seen.last().cloned(),
            })
        }
        "tutor_chat" => {
            let context: engine_core::chat::Context =
                serde_json::from_value(input["context"].clone())?;
            let prompt = engine_core::chat::prompt(&context);
            let started = std::time::Instant::now();
            let (mut stream, route) = engine_core::eval::stream_bounded_routed(
                router,
                "chat",
                &prompt,
                None,
                std::time::Duration::from_secs(20),
                std::time::Duration::from_secs(20),
            )
            .await?;
            let (mut reply, mut usage, mut terminal) = (String::new(), None, "end");
            while let Some(event) = stream.next().await {
                match event {
                    Ok(engine_core::StreamEvent::Delta(d)) => reply.push_str(&d),
                    Ok(engine_core::StreamEvent::Usage(u)) => usage = Some(u),
                    Err(_) => {
                        terminal = "error";
                        break;
                    }
                }
            }
            let latency = u32::try_from(started.elapsed().as_millis()).unwrap_or(u32::MAX);
            let model = usage
                .as_ref()
                .map(|u| u.model.clone())
                .unwrap_or(route.model);
            let attempts = vec![attempt(
                usage.as_ref(),
                &model,
                0,
                route.fallback_used,
                latency,
            )];
            Ok(Live {
                text: json!({"terminal": terminal, "reply": reply}).to_string(),
                attempts,
                completion: usage,
            })
        }
        other => anyhow::bail!("no live evaluator for {other}"),
    }
}

fn attempt(
    c: Option<&Completion>,
    model: &str,
    index: usize,
    fallback: bool,
    latency: u32,
) -> Attempt {
    Attempt {
        provider: c.map(|c| c.provider.clone()).unwrap_or_default(),
        model: model.into(),
        input_tokens: c.map_or(0, |c| c.input_tokens),
        output_tokens: c.map_or(0, |c| c.output_tokens),
        cache_write_tokens: c.map_or(0, |c| c.cache_write_tokens),
        cache_read_tokens: c.map_or(0, |c| c.cache_read_tokens),
        latency_ms: latency,
        attempt: index as u32 + 1,
        fallback_used: fallback,
        delivered: c.is_some(),
        usage_unavailable: c.is_none_or(|c| c.usage_unavailable),
        error_code: None,
    }
}

/// Case shape of a course outline (`CourseOutline` in the proto contract).
#[derive(serde::Deserialize)]
struct Outline {
    title: String,
    #[serde(default)]
    description: String,
    units: Vec<engine_core::courses::outline::Unit>,
}

impl From<Outline> for engine_proto::CourseOutline {
    fn from(o: Outline) -> Self {
        Self {
            course_id: String::new(),
            title: o.title,
            description: o.description,
            units: o
                .units
                .into_iter()
                .map(|u| engine_proto::Unit {
                    unit_id: u.id,
                    title: u.title,
                    lessons: u
                        .lessons
                        .into_iter()
                        .map(|l| engine_proto::LessonRef {
                            lesson_id: l.id,
                            title: l.title,
                            goal: l.goal,
                        })
                        .collect(),
                })
                .collect(),
        }
    }
}
