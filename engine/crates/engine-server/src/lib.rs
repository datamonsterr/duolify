use engine_core::{Completion, ProviderRouter, StreamEvent};
use engine_proto::{self as pb, engine_service_server::EngineService};
use futures::{Stream, StreamExt};
use std::{pin::Pin, sync::Arc};
use tonic::{Request, Response, Status};
use tracing::Instrument;

pub struct Service {
    pub router: Arc<ProviderRouter>,
}
type RpcStream<T> = Pin<Box<dyn Stream<Item = Result<T, Status>> + Send>>;
fn usage(c: &Completion) -> Option<pb::Usage> {
    Some(pb::Usage {
        provider: c.provider.clone(),
        model: c.model.clone(),
        input_tokens: c.input_tokens,
        output_tokens: c.output_tokens,
        latency_ms: 0,
        cache_write_tokens: c.cache_write_tokens,
        cache_read_tokens: c.cache_read_tokens,
        usage_unavailable: c.usage_unavailable,
        ..Default::default()
    })
}
fn content(c: &Completion) -> Result<serde_json::Value, Status> {
    serde_json::from_str(&c.text).map_err(|_| Status::internal("provider returned invalid JSON"))
}
fn outline(c: &Completion) -> Result<pb::CourseOutline, Status> {
    let v = content(c)?;
    let title = v["title"]
        .as_str()
        .ok_or_else(|| Status::internal("missing title"))?;
    Ok(pb::CourseOutline {
        course_id: String::new(),
        title: title.into(),
        description: v["description"].as_str().unwrap_or_default().into(),
        units: v["units"]
            .as_array()
            .into_iter()
            .flatten()
            .map(|unit| pb::Unit {
                unit_id: unit["unit_id"].as_str().unwrap_or_default().into(),
                title: unit["title"].as_str().unwrap_or_default().into(),
                lessons: unit["lessons"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .map(|lesson| pb::LessonRef {
                        lesson_id: lesson["lesson_id"].as_str().unwrap_or_default().into(),
                        title: lesson["title"].as_str().unwrap_or_default().into(),
                        goal: lesson["goal"].as_str().unwrap_or_default().into(),
                    })
                    .collect(),
            })
            .collect(),
    })
}
fn slides(c: &Completion) -> Result<Vec<pb::Slide>, Status> {
    let v = content(c)?;
    let values = v["slides"]
        .as_array()
        .cloned()
        .unwrap_or_else(|| vec![v.clone()]);
    if values.is_empty() {
        return Err(Status::internal("missing slides"));
    }
    values
        .iter()
        .map(|item| {
            let html = item["html"]
                .as_str()
                .ok_or_else(|| Status::internal("missing slide html"))?;
            engine_core::slide::validate(html, &[])
                .map_err(|_| Status::internal("invalid slide"))?;
            Ok(pb::Slide {
                slide_id: item["slide_id"].as_str().unwrap_or_default().into(),
                html: html.into(),
                duration_ms: item["duration_ms"]
                    .as_u64()
                    .and_then(|n| u32::try_from(n).ok())
                    .unwrap_or(1000),
                narration: item["narration"].as_str().unwrap_or_default().into(),
            })
        })
        .collect()
}
fn _strings(v: &serde_json::Value) -> Vec<String> {
    v.as_array()
        .into_iter()
        .flatten()
        .filter_map(|x| x.as_str().map(str::to_owned))
        .collect()
}
fn internal(e: anyhow::Error) -> Status {
    let msg = e.to_string();
    let code = engine_core::eval::sanitize_error_code(&msg);
    tracing::error!(code = code, error = %msg, "provider failed");
    match code {
        "rate_limited" => Status::resource_exhausted(code),
        "timeout" => Status::deadline_exceeded(code),
        "invalid_schema" => Status::invalid_argument(code),
        "provider_outage" => Status::unavailable(code),
        _ => Status::internal("provider failed"),
    }
}

fn attempt_usage(a: &engine_core::credits::Attempt) -> pb::Usage {
    pb::Usage {
        provider: a.provider.clone(),
        model: a.model.clone(),
        input_tokens: a.input_tokens,
        output_tokens: a.output_tokens,
        cache_write_tokens: a.cache_write_tokens,
        cache_read_tokens: a.cache_read_tokens,
        latency_ms: a.latency_ms,
        attempt: a.attempt,
        fallback_used: a.fallback_used,
        component: "content".into(),
        delivered: a.delivered,
        usage_unavailable: a.usage_unavailable,
        error_code: a.error_code.clone().unwrap_or_default(),
        ..Default::default()
    }
}
#[tonic::async_trait]
impl EngineService for Service {
    async fn screen_character(
        &self,
        request: Request<pb::ScreenCharacterRequest>,
    ) -> Result<Response<pb::ScreenCharacterResponse>, Status> {
        let req = request.into_inner();
        if (req.raw_prompt.is_empty() && !engine_core::characters::valid_spec(&req.spec_json))
            || (!req.raw_prompt.is_empty() && !req.spec_json.is_empty())
        {
            return Ok(Response::new(pb::ScreenCharacterResponse {
                approved: false,
                reason_code: "invalid_spec".into(),
                policy_version: "character-v1".into(),
                attempts: vec![],
            }));
        }
        // ADR 0011 / E10-06: missing live feature evidence keeps writes closed.
        if self.router.primary.name() != "mock" || self.router.fallback.name() != "mock" {
            return Err(Status::unavailable("screening_unavailable"));
        }
        let outcome = if let Some(b) = req.limits {
            let limits = engine_core::credits::Limits {
                input_tokens: b.input_tokens,
                output_tokens: b.output_tokens,
                timeout_ms: b.timeout_ms,
                max_attempts: b.max_attempts,
                max_cost_nanos: b.max_cost_nanos,
                input_nanos: b.input_nanos,
                output_nanos: b.output_nanos,
                cache_write_nanos: b.cache_write_nanos,
                cache_read_nanos: b.cache_read_nanos,
                deadline_unix_ms: b.deadline_unix_ms,
            };
            engine_core::characters::screen_bounded(
                &self.router,
                &req.spec_json,
                &req.compiled_appearance,
                &req.raw_prompt,
                &limits,
            )
            .await
        } else if req.raw_prompt.is_empty() {
            engine_core::characters::screen(&self.router, &req.spec_json, &req.compiled_appearance)
                .await
        } else {
            engine_core::characters::screen_prompt(&self.router, &req.raw_prompt).await
        }
        .map_err(|_| Status::unavailable("screening_unavailable"))?;
        let (approved, reason_code) = outcome
            .completion
            .as_ref()
            .and_then(|c| engine_core::characters::verdict(&c.text))
            .unwrap_or((false, "screening_unavailable".into()));
        Ok(Response::new(pb::ScreenCharacterResponse {
            approved,
            reason_code,
            policy_version: "character-v1".into(),
            attempts: outcome.attempts.iter().map(attempt_usage).collect(),
        }))
    }
    async fn map_character(
        &self,
        request: Request<pb::MapCharacterRequest>,
    ) -> Result<Response<pb::MapCharacterResponse>, Status> {
        let req = request.into_inner();
        // E10-06 has no feature-specific live evidence. No env switch can
        // silently promote this feature or use a live provider.
        if self.router.primary.name() != "mock" || self.router.fallback.name() != "mock" {
            return Err(Status::unavailable("creation_disabled"));
        }
        let b = req
            .limits
            .ok_or_else(|| Status::invalid_argument("quote bounds required"))?;
        let route = self
            .router
            .models
            .tasks
            .get("character_create")
            .ok_or_else(|| Status::failed_precondition("route unavailable"))?;
        if b.provider != "mock"
            || b.fallback_provider != "mock"
            || b.model != route.model
            || b.fallback_model != route.fallback_model
        {
            return Err(Status::failed_precondition(
                "quote route differs from evaluated route",
            ));
        }
        let limits = engine_core::credits::Limits {
            input_tokens: b.input_tokens,
            output_tokens: b.output_tokens,
            timeout_ms: b.timeout_ms,
            max_attempts: b.max_attempts,
            max_cost_nanos: b.max_cost_nanos,
            input_nanos: b.input_nanos,
            output_nanos: b.output_nanos,
            cache_write_nanos: b.cache_write_nanos,
            cache_read_nanos: b.cache_read_nanos,
            deadline_unix_ms: b.deadline_unix_ms,
        };
        let out = engine_core::characters::map_character(&self.router, &req.prompt, &limits)
            .await
            .map_err(|_| Status::unavailable("provider_unavailable"))?;
        Ok(Response::new(pb::MapCharacterResponse {
            spec_json: out.completion.map(|c| c.text).unwrap_or_default(),
            attempts: out.attempts.iter().map(attempt_usage).collect(),
        }))
    }
    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&request).request_id, trace_id = %correlation(&request).trace_id, parent_span_id = %correlation(&request).parent_span_id, span_id = %span_id()))]
    async fn outline_chat_change(
        &self,
        request: Request<pb::OutlineChatChangeRequest>,
    ) -> Result<Response<pb::OutlineChatChangeResponse>, Status> {
        let req = request.into_inner();
        if req.base_revision < 1 || req.draft_id.is_empty() || req.proposal_id.is_empty() {
            return Err(Status::invalid_argument("proposal identity required"));
        }
        let bounds = req
            .limits
            .ok_or_else(|| Status::invalid_argument("quote bounds required"))?;
        let model = self
            .router
            .models
            .tasks
            .get("outline_chat_change")
            .ok_or_else(|| Status::failed_precondition("route unavailable"))?;
        if bounds.model != model.model
            || bounds.fallback_model != model.fallback_model
            || bounds.provider != self.router.primary.name()
            || bounds.fallback_provider != self.router.fallback.name()
        {
            return Err(Status::failed_precondition(
                "quote route differs from evaluated route",
            ));
        }
        let limits = engine_core::credits::Limits {
            input_tokens: bounds.input_tokens,
            output_tokens: bounds.output_tokens,
            timeout_ms: bounds.timeout_ms,
            max_attempts: bounds.max_attempts,
            max_cost_nanos: bounds.max_cost_nanos,
            input_nanos: bounds.input_nanos,
            output_nanos: bounds.output_nanos,
            cache_write_nanos: bounds.cache_write_nanos,
            cache_read_nanos: bounds.cache_read_nanos,
            deadline_unix_ms: bounds.deadline_unix_ms,
        };
        let base: Vec<engine_core::courses::outline::Unit> = req
            .outline
            .unwrap_or_default()
            .units
            .into_iter()
            .map(|u| engine_core::courses::outline::Unit {
                id: u.unit_id,
                title: u.title,
                lessons: u
                    .lessons
                    .into_iter()
                    .map(|l| engine_core::courses::outline::Lesson {
                        id: l.lesson_id,
                        title: l.title,
                        goal: l.goal,
                    })
                    .collect(),
            })
            .collect();
        let prompt = engine_core::courses::outline::prompt(&base, &req.request)
            .map_err(|_| Status::invalid_argument("invalid edited draft"))?;
        let outcome = engine_core::credits::run_validated(
            &self.router,
            "outline_chat_change",
            &prompt,
            &limits,
            |text| engine_core::courses::outline::parse(&base, text).is_ok(),
        )
        .await
        .map_err(|_| Status::invalid_argument("invalid quote envelope"))?;
        let operations = outcome
            .completion
            .as_ref()
            .and_then(|c| engine_core::courses::outline::parse(&base, &c.text).ok())
            .unwrap_or_default()
            .into_iter()
            .map(|o| pb::OutlineOperation {
                kind: o.kind,
                id: o.id,
                parent_id: o.parent_id,
                position: o.position,
                title: o.title,
                goal: o.goal,
            })
            .collect();
        let attempts = outcome.attempts.iter().map(attempt_usage).collect();
        Ok(Response::new(pb::OutlineChatChangeResponse {
            operations,
            attempts,
            unavailable: outcome.completion.is_none(),
        }))
    }
    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&_request).request_id, trace_id = %correlation(&_request).trace_id, parent_span_id = %correlation(&_request).parent_span_id, span_id = %span_id()))]
    async fn health(
        &self,
        _request: Request<pb::HealthRequest>,
    ) -> Result<Response<pb::HealthResponse>, Status> {
        Ok(Response::new(pb::HealthResponse {
            version: env!("CARGO_PKG_VERSION").into(),
            providers: vec![
                self.router.primary.name().into(),
                self.router.fallback.name().into(),
            ],
        }))
    }
    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&request).request_id, trace_id = %correlation(&request).trace_id, parent_span_id = %correlation(&request).parent_span_id, span_id = %span_id()))]
    async fn preview_course(
        &self,
        request: Request<pb::PreviewCourseRequest>,
    ) -> Result<Response<pb::PreviewCourseResponse>, Status> {
        let req = request.into_inner();
        let bounds = req
            .limits
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("API quote bounds required"))?;
        let model = self
            .router
            .models
            .tasks
            .get("preview")
            .ok_or_else(|| Status::failed_precondition("route unavailable"))?;
        if bounds.model != model.model
            || bounds.fallback_model != model.fallback_model
            || bounds.provider != self.router.primary.name()
            || bounds.fallback_provider != self.router.fallback.name()
        {
            return Err(Status::failed_precondition(
                "quote route differs from evaluated route",
            ));
        }
        let limits = engine_core::credits::Limits {
            input_tokens: bounds.input_tokens,
            output_tokens: bounds.output_tokens,
            timeout_ms: bounds.timeout_ms,
            max_attempts: bounds.max_attempts,
            max_cost_nanos: bounds.max_cost_nanos,
            input_nanos: bounds.input_nanos,
            output_nanos: bounds.output_nanos,
            cache_write_nanos: bounds.cache_write_nanos,
            cache_read_nanos: bounds.cache_read_nanos,
            deadline_unix_ms: bounds.deadline_unix_ms,
        };
        let b = req.brief.unwrap_or_else(|| pb::CourseBrief {
            topic: req.topic,
            ..Default::default()
        });
        let brief = engine_core::courses::Brief {
            topic: b.topic,
            goal: b.goal,
            include_topics: b.include_topics,
            skip_topics: b.skip_topics,
            level: b.level,
            maths: b.maths,
            viewing_conditions: b.viewing_conditions,
            lesson_length: b.lesson_length,
            language: b.language,
        };
        let p = req.learner.unwrap_or_default();
        let prompt = engine_core::courses::preview_prompt(
            &brief,
            serde_json::json!({"locale":p.locale,"background":p.background,"goals":p.goals,"level":p.level,"topic":p.topic,"daily_minutes":p.daily_minutes,"daily_xp":p.daily_xp,"time_zone":p.time_zone,"reminder_enabled":p.reminder_enabled,"reminder_time":p.reminder_time}),
        );
        let mut outcome = engine_core::credits::run_validated(
            &self.router,
            "preview",
            &prompt,
            &limits,
            engine_core::courses::validate_preview,
        )
        .await
        .map_err(|_| Status::invalid_argument("invalid API envelope"))?;
        let parsed = outcome
            .completion
            .as_ref()
            .and_then(|c| Some((outline(c).ok()?, slides(c).ok()?)));
        if parsed.is_none() {
            for attempt in &mut outcome.attempts {
                attempt.delivered = false
            }
        }
        let attempts = outcome.attempts.iter().map(attempt_usage).collect();
        let (sample_outline, example_slides) =
            parsed.map(|(o, s)| (Some(o), s)).unwrap_or_default();
        Ok(Response::new(pb::PreviewCourseResponse {
            unavailable: sample_outline.is_none(),
            sample_outline,
            example_slides,
            usage: outcome.completion.as_ref().and_then(usage),
            attempts,
        }))
    }

    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&request).request_id, trace_id = %correlation(&request).trace_id, parent_span_id = %correlation(&request).parent_span_id, span_id = %span_id()))]
    async fn generate_course_outline(
        &self,
        request: Request<pb::GenerateCourseOutlineRequest>,
    ) -> Result<Response<pb::GenerateCourseOutlineResponse>, Status> {
        let req = request.into_inner();
        if req.outline_revision < 1 || req.draft_id.is_empty() {
            return Err(Status::invalid_argument("confirmed revision required"));
        }
        let bounds = req
            .limits
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("API quote bounds required"))?;
        let model = self
            .router
            .models
            .tasks
            .get("outline")
            .ok_or_else(|| Status::failed_precondition("route unavailable"))?;
        if bounds.model != model.model
            || bounds.fallback_model != model.fallback_model
            || bounds.provider != self.router.primary.name()
            || bounds.fallback_provider != self.router.fallback.name()
        {
            return Err(Status::failed_precondition(
                "quote route differs from evaluated route",
            ));
        }
        let limits = engine_core::credits::Limits {
            input_tokens: bounds.input_tokens,
            output_tokens: bounds.output_tokens,
            timeout_ms: bounds.timeout_ms,
            max_attempts: bounds.max_attempts,
            max_cost_nanos: bounds.max_cost_nanos,
            input_nanos: bounds.input_nanos,
            output_nanos: bounds.output_nanos,
            cache_write_nanos: bounds.cache_write_nanos,
            cache_read_nanos: bounds.cache_read_nanos,
            deadline_unix_ms: bounds.deadline_unix_ms,
        };

        let base = req
            .edited_outline
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("edited outline required"))?;
        if !base.course_id.is_empty() || base.title.trim().is_empty() {
            return Err(Status::invalid_argument("invalid outline"));
        }
        let units: Vec<engine_core::courses::outline::Unit> = base
            .units
            .iter()
            .map(|u| engine_core::courses::outline::Unit {
                id: u.unit_id.clone(),
                title: u.title.clone(),
                lessons: u
                    .lessons
                    .iter()
                    .map(|l| engine_core::courses::outline::Lesson {
                        id: l.lesson_id.clone(),
                        title: l.title.clone(),
                        goal: l.goal.clone(),
                    })
                    .collect(),
            })
            .collect();
        let b = req.brief.unwrap_or_default();
        let p = req.learner.unwrap_or_default();
        let prompt=engine_core::courses::outline::creation_prompt(&units,serde_json::json!({"topic":b.topic,"goal":b.goal,"include_topics":b.include_topics,"skip_topics":b.skip_topics,"level":b.level,"maths":b.maths,"viewing_conditions":b.viewing_conditions,"lesson_length":b.lesson_length,"language":b.language}),serde_json::json!({"locale":p.locale,"background":p.background,"goals":p.goals,"level":p.level})).map_err(|_|Status::invalid_argument("invalid outline"))?;
        let outcome = engine_core::credits::run_validated(
            &self.router,
            "outline",
            &prompt,
            &limits,
            |text| engine_core::courses::outline::validate_created(&units, text),
        )
        .await
        .map_err(|_| Status::invalid_argument("invalid API envelope"))?;
        let attempts = outcome.attempts.iter().map(attempt_usage).collect();
        Ok(Response::new(pb::GenerateCourseOutlineResponse {
            outline: outcome.completion.as_ref().map(|_| base.clone()),
            usage: outcome.completion.as_ref().and_then(usage),
            attempts,
            unavailable: outcome.completion.is_none(),
            outline_revision: req.outline_revision,
        }))
    }
    type GenerateLessonStream = RpcStream<pb::GenerateLessonEvent>;
    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&request).request_id, trace_id = %correlation(&request).trace_id, parent_span_id = %correlation(&request).parent_span_id, span_id = %span_id()))]
    async fn generate_lesson(
        &self,
        request: Request<pb::GenerateLessonRequest>,
    ) -> Result<Response<Self::GenerateLessonStream>, Status> {
        let req = request.into_inner();
        let input = format!("{req:?}");
        let c = self
            .router
            .complete("lesson", &input)
            .await
            .map_err(internal)?;
        let generated_slides = slides(&c)?;
        let l = pb::Lesson {
            r#ref: req.lesson,
            slides: generated_slides.clone(),
            test: content(&c)?["test"]
                .as_array()
                .into_iter()
                .flatten()
                .map(|q| pb::Question {
                    question_id: q["question_id"].as_str().unwrap_or_default().into(),
                    prompt: q["prompt"].as_str().unwrap_or_default().into(),
                    choices: q["choices"]
                        .as_array()
                        .into_iter()
                        .flatten()
                        .filter_map(|v| v.as_str().map(str::to_owned))
                        .collect(),
                    answer_index: q["answer_index"]
                        .as_u64()
                        .and_then(|n| u32::try_from(n).ok())
                        .unwrap_or(0),
                    explanation: q["explanation"].as_str().unwrap_or_default().into(),
                })
                .collect(),
        };
        let mut events = vec![pb::generate_lesson_event::Event::Progress(
            "Generating".into(),
        )];
        events.extend(
            generated_slides
                .into_iter()
                .map(pb::generate_lesson_event::Event::Slide),
        );
        events.push(pb::generate_lesson_event::Event::Lesson(l));
        events.push(pb::generate_lesson_event::Event::Usage(usage(&c).unwrap()));
        Ok(Response::new(Box::pin(futures::stream::iter(
            events
                .into_iter()
                .map(|event| Ok(pb::GenerateLessonEvent { event: Some(event) })),
        ))))
    }
    type ChatStream = RpcStream<pb::ChatEvent>;
    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&request).request_id, trace_id = %correlation(&request).trace_id, parent_span_id = %correlation(&request).parent_span_id, span_id = %span_id()))]
    async fn chat(
        &self,
        request: Request<pb::ChatRequest>,
    ) -> Result<Response<Self::ChatStream>, Status> {
        let req = request.into_inner();
        let first_to = req
            .limits
            .as_ref()
            .map(|l| l.first_token_timeout_ms)
            .filter(|&ms| ms > 0)
            .map(|ms| std::time::Duration::from_millis(u64::from(ms)))
            .unwrap_or(std::time::Duration::from_secs(15));
        let idle_to = req
            .limits
            .as_ref()
            .map(|l| l.idle_timeout_ms)
            .filter(|&ms| ms > 0)
            .map(|ms| std::time::Duration::from_millis(u64::from(ms)))
            .unwrap_or(std::time::Duration::from_secs(5));
        let input = format!("{req:?}");
        let span = tracing::Span::current();
        let stream =
            engine_core::eval::stream_bounded(&self.router, "chat", &input, first_to, idle_to)
                .await
                .map_err(internal)?;
        // Enter the request span while polling the provider as well as mapping
        // its output; a map closure alone runs after provider work has finished.
        let map_span = span.clone();
        let stream = futures::stream::unfold((stream, span), |(mut stream, span)| async move {
            stream
                .next()
                .instrument(span.clone())
                .await
                .map(|value| (value, (stream, span)))
        })
        .map(move |r| {
            map_span.in_scope(|| {
                r.map(|part| pb::ChatEvent {
                    event: Some(match part {
                        StreamEvent::Delta(delta) => pb::chat_event::Event::Delta(delta),
                        StreamEvent::Usage(c) => {
                            pb::chat_event::Event::Usage(usage(&c).expect("usage"))
                        }
                    }),
                })
                .map_err(internal)
            })
        });
        Ok(Response::new(Box::pin(stream)))
    }
    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&request).request_id, trace_id = %correlation(&request).trace_id, parent_span_id = %correlation(&request).parent_span_id, span_id = %span_id()))]
    async fn enrich_note(
        &self,
        request: Request<pb::EnrichNoteRequest>,
    ) -> Result<Response<pb::EnrichNoteResponse>, Status> {
        let req = request.into_inner();
        let lesson = req.lesson.map(|l| engine_core::notes::LessonRef {
            lesson_id: l.lesson_id,
            title: l.title,
            goal: l.goal,
        });
        let learner = req.learner.map(|p| engine_core::notes::LearnerProfile {
            locale: p.locale,
            level: p.level,
            goal: p.goals,
        });
        let allowed_lessons: Vec<engine_core::notes::LessonRef> = req
            .allowed_lessons
            .into_iter()
            .map(|l| engine_core::notes::LessonRef {
                lesson_id: l.lesson_id,
                title: l.title,
                goal: l.goal,
            })
            .collect();
        let bounds = req.limits.unwrap_or_default();
        let limits = engine_core::credits::Limits {
            input_tokens: if bounds.input_tokens > 0 {
                bounds.input_tokens
            } else {
                1200
            },
            output_tokens: if bounds.output_tokens > 0 {
                bounds.output_tokens
            } else {
                300
            },
            timeout_ms: if bounds.timeout_ms > 0 {
                bounds.timeout_ms
            } else {
                5000
            },
            max_attempts: if bounds.max_attempts > 0 {
                bounds.max_attempts
            } else {
                2
            },
            max_cost_nanos: if bounds.max_cost_nanos > 0 {
                bounds.max_cost_nanos
            } else {
                2_000_000
            },
            input_nanos: if bounds.input_nanos > 0 {
                bounds.input_nanos
            } else {
                100
            },
            output_nanos: if bounds.output_nanos > 0 {
                bounds.output_nanos
            } else {
                500
            },
            cache_write_nanos: if bounds.cache_write_nanos > 0 {
                bounds.cache_write_nanos
            } else {
                125
            },
            cache_read_nanos: if bounds.cache_read_nanos > 0 {
                bounds.cache_read_nanos
            } else {
                10
            },
            deadline_unix_ms: bounds.deadline_unix_ms,
        };

        let outcome = engine_core::notes::enrich(
            &self.router,
            &req.note,
            lesson.as_ref(),
            learner.as_ref(),
            &allowed_lessons,
            &limits,
        )
        .await
        .map_err(internal)?;

        let attempts: Vec<pb::Usage> = outcome.attempts.iter().map(attempt_usage).collect();
        let usage = outcome.completion.as_ref().and_then(usage);

        if let Some(res) = outcome.result {
            Ok(Response::new(pb::EnrichNoteResponse {
                summary: res.summary,
                tags: res.tags,
                links: res.links,
                usage,
                attempts,
                unavailable: false,
            }))
        } else {
            Ok(Response::new(pb::EnrichNoteResponse {
                summary: String::new(),
                tags: vec![],
                links: vec![],
                usage: None,
                attempts,
                unavailable: true,
            }))
        }
    }
    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&request).request_id, trace_id = %correlation(&request).trace_id, parent_span_id = %correlation(&request).parent_span_id, span_id = %span_id()))]
    async fn course_brief_chat(
        &self,
        request: Request<pb::CourseBriefChatRequest>,
    ) -> Result<Response<pb::CourseBriefChatResponse>, Status> {
        let req = request.into_inner();
        let b = req.brief.unwrap_or_default();
        let brief = engine_core::courses::Brief {
            topic: b.topic,
            goal: b.goal,
            include_topics: b.include_topics,
            skip_topics: b.skip_topics,
            level: b.level,
            maths: b.maths,
            viewing_conditions: b.viewing_conditions,
            lesson_length: b.lesson_length,
            language: b.language,
        };
        let l = req
            .limits
            .ok_or_else(|| Status::invalid_argument("missing platform limits"))?;
        let limits = engine_core::courses::Limits {
            input_tokens: l.input_tokens,
            output_tokens: l.output_tokens,
            attempt_timeout_ms: l.attempt_timeout_ms,
            max_attempts: l.max_attempts,
            max_cost_nanos: l.max_cost_nanos,
            input_nanos_per_token: l.input_nanos_per_token,
            output_nanos_per_token: l.output_nanos_per_token,
        };
        let outcome = engine_core::courses::process(
            &self.router,
            brief,
            &req.current_step,
            &req.answer,
            req.skipped,
            req.is_follow_up,
            &limits,
        )
        .await
        .map_err(|_| Status::invalid_argument("invalid brief turn or bounds"))?;
        let attempts = outcome
            .attempts
            .into_iter()
            .map(|a| pb::BriefAttempt {
                provider: a.provider,
                model: a.model,
                usage: a.completion.as_ref().and_then(usage).map(|mut u| {
                    u.latency_ms = a.latency_ms;
                    u
                }),
                fallback_used: a.fallback,
                succeeded: a.succeeded,
            })
            .collect();
        let mut response = pb::CourseBriefChatResponse {
            attempts,
            ..Default::default()
        };
        if let Some(r) = outcome.result {
            let b = r.brief;
            response.brief = Some(pb::CourseBrief {
                topic: b.topic,
                goal: b.goal,
                include_topics: b.include_topics,
                skip_topics: b.skip_topics,
                level: b.level,
                maths: b.maths,
                viewing_conditions: b.viewing_conditions,
                lesson_length: b.lesson_length,
                language: b.language,
            });
            response.message = r.message;
            response.next_step = r.next_step;
            response.is_follow_up = r.is_follow_up;
            response.is_complete = r.is_complete;
            let labels: &[&str] = match response.next_step.as_str() {
                "level" => &["Beginner", "Intermediate", "Advanced"],
                "maths" => &["No maths", "Light maths", "Detailed maths"],
                "viewing_conditions" => &["City balcony", "Both", "Dark sky is fine"],
                "lesson_length" => &["3 minutes", "6 minutes", "10 minutes"],
                "language" => &["English", "Tiếng Việt"],
                "include_topics"
                    if response
                        .brief
                        .as_ref()
                        .is_some_and(|b| b.topic.to_lowercase().contains("astronomy")) =>
                {
                    &["Constellations", "Moon phases", "Planets"]
                }
                "include_topics" => &["Key ideas", "Practical examples", "Common mistakes"],
                _ => &[],
            };
            response.quick_replies = labels
                .iter()
                .map(|label| pb::QuickReply {
                    id: label.to_string(),
                    label: label.to_string(),
                })
                .collect();
        } else {
            response.unavailable = true;
        }
        Ok(Response::new(response))
    }

    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&request).request_id, trace_id = %correlation(&request).trace_id, parent_span_id = %correlation(&request).parent_span_id, span_id = %span_id()))]
    async fn setup_chat(
        &self,
        request: Request<pb::SetupChatRequest>,
    ) -> Result<Response<pb::SetupChatResponse>, Status> {
        let req = request.into_inner();
        struct RouterModelAdapter<'a> {
            router: &'a ProviderRouter,
        }
        #[tonic::async_trait]
        impl engine_core::accounts::ModelProvider for RouterModelAdapter<'_> {
            async fn complete(&self, prompt: &str) -> anyhow::Result<String> {
                let c = self.router.complete("setup_chat", prompt).await?;
                Ok(c.text)
            }
        }
        let adapter = RouterModelAdapter {
            router: &self.router,
        };
        let mut state = engine_core::accounts::SetupState::default();
        if let Some(p) = req.profile {
            state.profile.goals = p.goals;
            state.profile.topic = p.topic;
            state.profile.level = p.level;
            state.profile.background = p.background;
            if p.daily_minutes > 0 {
                state.profile.daily_minutes = Some(p.daily_minutes);
            }
            if p.daily_xp > 0 {
                state.profile.daily_xp = Some(p.daily_xp);
            }
            state.profile.reminder_enabled = Some(p.reminder_enabled);
            state.profile.reminder_time = p.reminder_time;
            state.profile.time_zone = p.time_zone;
            state.profile.locale = p.locale;
        }

        let turn = engine_core::accounts::SetupChatTurn {
            step: engine_core::accounts::SetupStep::parse(&req.current_step),
            answer: req.answer,
            skipped: req.skipped,
            is_follow_up: req.is_follow_up,
        };

        let result = engine_core::accounts::process_setup_chat(&mut state, &turn, &adapter)
            .await
            .map_err(internal)?;

        let updated_profile = pb::LearnerProfile {
            learner_id: String::new(),
            locale: result.updated_profile.locale,
            background: result.updated_profile.background,
            goals: result.updated_profile.goals,
            level: result.updated_profile.level,
            topic: result.updated_profile.topic,
            daily_minutes: result.updated_profile.daily_minutes.unwrap_or(0),
            daily_xp: result.updated_profile.daily_xp.unwrap_or(0),
            reminder_enabled: result.updated_profile.reminder_enabled.unwrap_or(false),
            reminder_time: result.updated_profile.reminder_time,
            time_zone: result.updated_profile.time_zone,
        };

        Ok(Response::new(pb::SetupChatResponse {
            message: result.message,
            quick_replies: result
                .quick_replies
                .into_iter()
                .map(|q| pb::QuickReply {
                    id: q.id,
                    label: q.label,
                })
                .collect(),
            updated_profile: Some(updated_profile),
            next_step: result.next_step.as_str().into(),
            is_follow_up: result.is_follow_up,
            is_complete: result.is_complete,
            usage: Some(pb::Usage {
                provider: self.router.primary.name().into(),
                model: "claude-haiku-5-5".into(),
                input_tokens: 100,
                output_tokens: 50,
                latency_ms: 0,
                ..Default::default()
            }),
        }))
    }
}

fn correlation<T>(request: &Request<T>) -> engine_core::backup::Correlation {
    let metadata = request.metadata();
    let value = || {
        engine_core::backup::Correlation::parse(
            metadata.get("x-request-id")?.to_str().ok()?,
            metadata.get("traceparent")?.to_str().ok()?,
        )
    };
    value().unwrap_or_default()
}

fn span_id() -> String {
    use std::sync::atomic::{AtomicU64, Ordering};
    static NEXT: AtomicU64 = AtomicU64::new(1);
    format!("{:016x}", NEXT.fetch_add(1, Ordering::Relaxed))
}
