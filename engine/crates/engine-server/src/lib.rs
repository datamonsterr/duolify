use engine_core::{Completion, ProviderRouter, StreamEvent};
use engine_proto::{self as pb, engine_service_server::EngineService};
use futures::{Stream, StreamExt};
use std::{pin::Pin, sync::Arc};
use tonic::{Request, Response, Status};

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
fn strings(v: &serde_json::Value) -> Vec<String> {
    v.as_array()
        .into_iter()
        .flatten()
        .filter_map(|x| x.as_str().map(str::to_owned))
        .collect()
}
fn internal(e: anyhow::Error) -> Status {
    tracing::error!(error = %e, "provider failed");
    Status::internal("provider failed")
}
#[tonic::async_trait]
impl EngineService for Service {
    async fn health(
        &self,
        _: Request<pb::HealthRequest>,
    ) -> Result<Response<pb::HealthResponse>, Status> {
        Ok(Response::new(pb::HealthResponse {
            version: env!("CARGO_PKG_VERSION").into(),
            providers: vec![
                self.router.primary.name().into(),
                self.router.fallback.name().into(),
            ],
        }))
    }
    async fn preview_course(
        &self,
        request: Request<pb::PreviewCourseRequest>,
    ) -> Result<Response<pb::PreviewCourseResponse>, Status> {
        let req = request.into_inner();
        let c = self
            .router
            .complete("preview", &format!("{req:?}"))
            .await
            .map_err(internal)?;
        Ok(Response::new(pb::PreviewCourseResponse {
            sample_outline: Some(outline(&c)?),
            example_slides: slides(&c)?,
            usage: usage(&c),
        }))
    }
    async fn generate_course_outline(
        &self,
        request: Request<pb::GenerateCourseOutlineRequest>,
    ) -> Result<Response<pb::GenerateCourseOutlineResponse>, Status> {
        let req = request.into_inner();
        let c = self
            .router
            .complete("outline", &format!("{req:?}"))
            .await
            .map_err(internal)?;
        Ok(Response::new(pb::GenerateCourseOutlineResponse {
            outline: Some(outline(&c)?),
            usage: usage(&c),
        }))
    }
    type GenerateLessonStream = RpcStream<pb::GenerateLessonEvent>;
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
    async fn chat(
        &self,
        request: Request<pb::ChatRequest>,
    ) -> Result<Response<Self::ChatStream>, Status> {
        let input = format!("{:?}", request.into_inner());
        let stream = self
            .router
            .stream("chat", &input)
            .await
            .map_err(internal)?
            .map(|r| {
                r.map(|part| pb::ChatEvent {
                    event: Some(match part {
                        StreamEvent::Delta(delta) => pb::chat_event::Event::Delta(delta),
                        StreamEvent::Usage(c) => {
                            pb::chat_event::Event::Usage(usage(&c).expect("usage"))
                        }
                    }),
                })
                .map_err(internal)
            });
        Ok(Response::new(Box::pin(stream)))
    }
    async fn enrich_note(
        &self,
        request: Request<pb::EnrichNoteRequest>,
    ) -> Result<Response<pb::EnrichNoteResponse>, Status> {
        let req = request.into_inner();
        let c = self
            .router
            .complete("note", &format!("{req:?}"))
            .await
            .map_err(internal)?;
        Ok(Response::new(pb::EnrichNoteResponse {
            summary: content(&c)?["summary"]
                .as_str()
                .ok_or_else(|| Status::internal("missing summary"))?
                .into(),
            tags: strings(&content(&c)?["tags"]),
            links: strings(&content(&c)?["links"]),
            usage: usage(&c),
        }))
    }
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
            }),
        }))
    }
}
