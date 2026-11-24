use engine_core::{Completion, ProviderRouter};
use engine_proto::{self as pb, engine_service_server::EngineService};
use futures::{Stream, StreamExt};
use std::{pin::Pin, sync::Arc};
use tonic::{Request, Response, Status};
use tracing::Instrument;

pub struct Service {
    pub router: Arc<ProviderRouter>,
    /// Narration provider; only the deterministic mock exists until E14-04.
    pub speech: Arc<dyn engine_core::narration::SpeechProvider>,
    /// E10-06 live promotion evidence. Fixture routers bypass it; a live route runs a task only
    /// when the gate reports a passing live case for it.
    pub promotion: Arc<engine_core::promotion::Gate>,
}
impl Service {
    /// Production wiring: the evidence embedded in this build.
    pub fn new(
        router: Arc<ProviderRouter>,
        speech: Arc<dyn engine_core::narration::SpeechProvider>,
    ) -> Self {
        Self {
            router,
            speech,
            promotion: Arc::new(engine_core::promotion::Gate::embedded()),
        }
    }
    /// Fails closed unless `task` may call a live model: fixture routers always pass, a live
    /// route needs passing promotion evidence for the task.
    fn live_model(&self, rpc: &'static str, task: &'static str) -> Result<(), Status> {
        if self.router.fixture_only() {
            return Ok(());
        }
        self.promotion
            .check(task)
            .map_err(|denial| not_promoted(rpc, task, &denial))
    }
    /// Same rule for narration: a non-mock speech vendor needs `lesson_narration` evidence.
    fn live_voice(&self, rpc: &'static str) -> Result<(), Status> {
        if self.speech.name() == "mock" {
            return Ok(());
        }
        self.promotion
            .check("lesson_narration")
            .map_err(|denial| not_promoted(rpc, "lesson_narration", &denial))
    }
}
/// Domain of the `google.rpc.ErrorInfo` detail on a not-promoted refusal.
pub const ERROR_DOMAIN: &str = "engine.duolify";
/// UNAVAILABLE with a typed `ErrorInfo` (reason = denial code, metadata = rpc and task) so the API
/// can log and alert on it while the learner sees a retryable "unavailable".
fn not_promoted(rpc: &str, task: &str, denial: &engine_core::promotion::Denial) -> Status {
    use tonic_types::{ErrorDetails, StatusExt};
    tracing::error!(
        event = "live_feature_not_promoted",
        rpc,
        task,
        reason = denial.code(),
        detail = %denial.detail(),
        "live model call refused: no passing promotion evidence"
    );
    Status::with_error_details(
        tonic::Code::Unavailable,
        denial.code(),
        ErrorDetails::with_error_info(
            denial.code(),
            ERROR_DOMAIN,
            [
                ("rpc".to_string(), rpc.to_string()),
                ("task".to_string(), task.to_string()),
            ],
        ),
    )
}
/// `ErrorInfo` domain of transcription failures. It differs from `ERROR_DOMAIN` so the API does not read
/// a failed or refused transcription as a missing promotion.
pub const TRANSCRIBE_ERROR_DOMAIN: &str = "engine.duolify.transcribe";
/// gRPC status with a typed `ErrorInfo` reason for a transcription refusal.
fn transcribe_status(error: engine_core::transcribe::TranscribeError) -> Status {
    use engine_core::transcribe::TranscribeError as E;
    use tonic_types::{ErrorDetails, StatusExt};
    let code = match error {
        E::InvalidAudio(_) | E::Rejected => tonic::Code::InvalidArgument,
        E::CostCeiling | E::Timeout | E::Failed => tonic::Code::Unavailable,
    };
    Status::with_error_details(
        code,
        error.reason(),
        ErrorDetails::with_error_info(
            error.reason(),
            TRANSCRIBE_ERROR_DOMAIN,
            [("rpc".to_string(), "Transcribe".to_string())],
        ),
    )
}
/// Logs the promotion state per gated task once at startup (alertable on error).
pub fn log_promotion_status(service: &Service) {
    let live = !service.router.fixture_only();
    match service.promotion.summary() {
        Ok(s) => tracing::info!(
            event = "live_promotion_evidence",
            live,
            dataset = %s.dataset,
            dataset_revision = %s.dataset_revision,
            passed_tasks = ?s.passed_tasks,
            "promotion evidence loaded"
        ),
        Err(d) if live => tracing::error!(
            event = "live_promotion_evidence",
            live,
            reason = d.code(),
            detail = %d.detail(),
            "no valid promotion evidence: every live AI feature fails closed"
        ),
        Err(d) => tracing::info!(
            event = "live_promotion_evidence",
            live,
            reason = d.code(),
            "fixture router: promotion evidence not required"
        ),
    }
    if live {
        for (rpc, task) in GATED {
            if let Err(d) = service.promotion.check(task) {
                tracing::error!(
                    event = "live_feature_not_promoted",
                    rpc,
                    task,
                    reason = d.code(),
                    "feature disabled on the live route"
                );
            }
        }
    }
    if service.speech.name() != "mock"
        && let Err(d) = service.promotion.check("lesson_narration")
    {
        tracing::error!(
            event = "live_feature_not_promoted",
            rpc = "SynthesizeNarration",
            task = "lesson_narration",
            reason = d.code(),
            "live narration disabled"
        );
    }
}
/// Every model-backed RPC and the Golden set task whose live pass enables it.
pub const GATED: [(&str, &str); 17] = [
    ("PreviewCourse", "course_preview"),
    ("GenerateCourseOutline", "course_outline"),
    ("GenerateLesson", "lesson_slides"),
    ("GenerateRetryVariants", "lesson_test"),
    ("Chat", "tutor_chat"),
    ("EnrichNote", "note_enrich"),
    ("SetupChat", "setup_chat"),
    ("CourseBriefChat", "course_brief_chat"),
    ("OutlineChatChange", "outline_chat_change"),
    ("ReplanOutline", "outline_replan"),
    ("ScreenInstruction", "steer_screen"),
    ("ScreenCharacter", "character_screen"),
    ("ScreenCourse", "course_screen"),
    ("ScreenReview", "screen_review"),
    ("MapCharacter", "character_create"),
    ("MapCharacter", "character_spec"),
    ("Transcribe", "transcribe"),
];
type RpcStream<T> = Pin<Box<dyn Stream<Item = Result<T, Status>> + Send>>;
fn usage(c: &Completion) -> Option<pb::Usage> {
    usage_estimated(c, false)
}
fn usage_estimated(c: &Completion, estimated: bool) -> Option<pb::Usage> {
    Some(pb::Usage {
        estimated,
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
/// Logs a caller-cancelled chat stream: gRPC drops the response stream when the API cancels, which
/// also drops the provider stream and so stops provider work.
struct CancelLog {
    span: tracing::Span,
    complete: bool,
}
impl CancelLog {
    fn new(span: tracing::Span) -> Self {
        Self {
            span,
            complete: false,
        }
    }
    fn complete(&mut self) {
        self.complete = true;
    }
}
impl Drop for CancelLog {
    fn drop(&mut self) {
        if !self.complete {
            self.span.in_scope(|| {
                tracing::info!(event = "chat_cancelled", "chat stream cancelled by caller")
            });
        }
    }
}
fn tutor_context(req: &pb::ChatRequest) -> engine_core::chat::Context {
    use engine_core::chat as c;
    let lesson = |l: &pb::LessonRef| c::LessonRef {
        id: l.lesson_id.clone(),
        title: l.title.clone(),
        goal: l.goal.clone(),
    };
    let outline = req.outline.clone().unwrap_or_default();
    let learner = req.learner.clone().unwrap_or_default();
    let persona = if req.tutor_persona_json.is_empty() {
        None
    } else {
        let parsed = engine_core::characters::tutor_persona(&req.tutor_persona_json);
        if parsed.is_none() {
            tracing::warn!(
                event = "tutor_persona_dropped",
                "invalid tutor persona ignored"
            );
        }
        parsed
    };
    let region = req.region_question.as_ref().map(|r| {
        let rect = r.rect.unwrap_or_default();
        c::RegionQuestion {
            slide_index: r.slide_index,
            rect: c::Rect {
                x: rect.x,
                y: rect.y,
                w: rect.w,
                h: rect.h,
            },
            at_ms: r.at_ms,
            whole_slide: r.whole_slide,
            regions: r
                .regions
                .iter()
                .map(|q| c::QuestionRegion {
                    region_id: q.region_id.clone(),
                    role: pb::RegionRole::try_from(q.role)
                        .map(|v| {
                            v.as_str_name()
                                .trim_start_matches("REGION_ROLE_")
                                .to_lowercase()
                        })
                        .unwrap_or_default(),
                    description: q.description.clone(),
                    overlap: q.overlap,
                })
                .collect(),
            slide_explanation: r.slide_explanation.clone(),
        }
    });
    c::Context {
        persona,
        region,
        course_title: outline.title,
        course_description: outline.description,
        units: outline
            .units
            .iter()
            .map(|u| c::Unit {
                id: u.unit_id.clone(),
                title: u.title.clone(),
                lessons: u.lessons.iter().map(lesson).collect(),
            })
            .collect(),
        current_lesson: req
            .current_lesson
            .as_ref()
            .and_then(|l| l.r#ref.as_ref())
            .map(lesson),
        profile: c::Profile {
            locale: learner.locale,
            background: learner.background,
            goals: learner.goals,
            level: learner.level,
            topic: learner.topic,
        },
        history: req.history.clone(),
        excerpts: req
            .excerpts
            .iter()
            .map(|e| c::Excerpt {
                lesson_id: e.lesson_id.clone(),
                lesson_title: e.lesson_title.clone(),
                text: e.text.clone(),
            })
            .collect(),
        messages: req
            .messages
            .iter()
            .map(|m| c::Message {
                role: m.role.clone(),
                content: m.content.clone(),
            })
            .collect(),
    }
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
/// `require_topics`: lesson Slides must teach a Topic; Course preview samples are standalone
/// examples (the `course_preview` schema has no `topic_ids`).
fn slides(c: &Completion, require_topics: bool) -> Result<Vec<pb::Slide>, Status> {
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
            let topic_ids: Vec<String> = item["topic_ids"]
                .as_array()
                .into_iter()
                .flatten()
                .filter_map(|t| t.as_str().map(str::to_owned))
                .collect();
            if require_topics {
                engine_core::lessontest::validate_slide_topics(&topic_ids).map_err(internal)?;
            }
            // Regions were validated with the Lesson; their elements are pinned to their slots.
            let regions = if item["regions"].is_array() {
                engine_core::regions::slide_regions(item)
                    .map_err(|_| Status::internal("invalid slide regions"))?
            } else {
                Vec::new()
            };
            Ok(pb::Slide {
                slide_id: item["slide_id"].as_str().unwrap_or_default().into(),
                html: engine_core::regions::layout(html, &regions),
                regions: regions.into_iter().map(region_proto).collect(),
                duration_ms: item["duration_ms"]
                    .as_u64()
                    .and_then(|n| u32::try_from(n).ok())
                    .unwrap_or(1000),
                narration: item["narration"].as_str().unwrap_or_default().into(),
                topic_ids,
                segments: item["segments"]
                    .as_array()
                    .into_iter()
                    .flatten()
                    .map(|s| pb::CharacterSegment {
                        speaker_key: s["speaker_key"].as_str().unwrap_or_default().into(),
                        text: s["text"].as_str().unwrap_or_default().into(),
                        expression: s["expression"].as_str().unwrap_or_default().into(),
                        pose: s["pose"].as_str().unwrap_or_default().into(),
                        start_ms: s["start_ms"].as_u64().unwrap_or(0) as u32,
                        duration_ms: s["duration_ms"].as_u64().unwrap_or(0) as u32,
                    })
                    .collect(),
            })
        })
        .collect()
}
fn region_proto(r: engine_core::regions::Region) -> pb::SlideRegion {
    let [x, y, w, h] = r.bbox;
    pb::SlideRegion {
        region_id: r.id,
        r#box: Some(pb::RegionBox { x, y, w, h }),
        window: r
            .window
            .map(|(start_ms, end_ms)| pb::RegionWindow { start_ms, end_ms }),
        role: pb::RegionRole::from_str_name(&format!("REGION_ROLE_{}", r.role.to_uppercase()))
            .unwrap_or_default() as i32,
        description: r.description,
    }
}
fn retry_question(q: &pb::Question) -> engine_core::lessontest::retry::Question {
    engine_core::lessontest::retry::Question {
        question_id: q.question_id.clone(),
        topic_id: q.topic_id.clone(),
        prompt: q.prompt.clone(),
        choices: q.choices.clone(),
        answer_index: q.answer_index,
        explanation: q.explanation.clone(),
        variant_of: q.variant_of.clone(),
    }
}
fn proto_question(q: engine_core::lessontest::retry::Question) -> pb::Question {
    pb::Question {
        question_id: q.question_id,
        topic_id: q.topic_id,
        prompt: q.prompt,
        choices: q.choices,
        answer_index: q.answer_index,
        explanation: q.explanation,
        variant_of: q.variant_of,
    }
}
fn lesson_limits(
    router: &ProviderRouter,
    b: &pb::UsageLimits,
) -> Result<engine_core::credits::Limits, Status> {
    let model = router
        .models
        .tasks
        .get("lesson")
        .ok_or_else(|| Status::failed_precondition("lesson route unavailable"))?;
    if b.model != model.model
        || b.fallback_model != model.fallback_model
        || b.provider != router.primary.name()
        || b.fallback_provider != router.fallback.name()
    {
        return Err(Status::failed_precondition(
            "quote route differs from evaluated route",
        ));
    }
    Ok(engine_core::credits::Limits {
        input_tokens: b.input_tokens,
        output_tokens: b.output_tokens,
        timeout_ms: b.timeout_ms,
        max_attempts: b.max_attempts.min(2),
        max_cost_nanos: b.max_cost_nanos,
        input_nanos: b.input_nanos,
        output_nanos: b.output_nanos,
        cache_write_nanos: b.cache_write_nanos,
        cache_read_nanos: b.cache_read_nanos,
        deadline_unix_ms: b.deadline_unix_ms,
    })
}
fn lesson_events(
    lesson: Option<pb::Lesson>,
    attempts: Vec<pb::Usage>,
) -> Response<RpcStream<pb::GenerateLessonEvent>> {
    let mut events: Vec<pb::generate_lesson_event::Event> = attempts
        .into_iter()
        .map(pb::generate_lesson_event::Event::Attempt)
        .collect();
    if let Some(l) = lesson {
        events.extend(
            l.slides
                .iter()
                .cloned()
                .map(pb::generate_lesson_event::Event::Slide),
        );
        events.push(pb::generate_lesson_event::Event::Lesson(l));
    } else {
        events.push(pb::generate_lesson_event::Event::Unavailable(true));
    }
    Response::new(Box::pin(futures::stream::iter(
        events
            .into_iter()
            .map(|event| Ok(pb::GenerateLessonEvent { event: Some(event) })),
    )))
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
    async fn screen_instruction(
        &self,
        request: Request<pb::ScreenInstructionRequest>,
    ) -> Result<Response<pb::ScreenInstructionResponse>, Status> {
        let req = request.into_inner();
        if let Some(reason) = engine_core::steering::deterministic(&req.text) {
            return Ok(Response::new(pb::ScreenInstructionResponse {
                allowed: false,
                reason_code: reason.into(),
                screening_version: "steering-v1".into(),
                attempts: vec![],
            }));
        }
        // E10-06: a live route screens only with passing live steer_screen evidence.
        self.live_model("ScreenInstruction", "steer_screen")?;
        let outcome = engine_core::steering::screen(&self.router, &req.text, &req.course_topic)
            .await
            .map_err(|_| Status::unavailable("screening_unavailable"))?;
        let (allowed, reason_code) = outcome
            .completion
            .as_ref()
            .and_then(|c| engine_core::steering::verdict(&c.text))
            .ok_or_else(|| Status::unavailable("screening_unavailable"))?;
        Ok(Response::new(pb::ScreenInstructionResponse {
            allowed,
            reason_code,
            screening_version: "steering-v1".into(),
            attempts: outcome.attempts.iter().map(attempt_usage).collect(),
        }))
    }
    async fn guard_input(
        &self,
        request: Request<pb::GuardInputRequest>,
    ) -> Result<Response<pb::GuardInputResponse>, Status> {
        use engine_core::guard::{self, Source, Surface};
        let req = request.into_inner();
        let surface = Surface::from_code(&req.surface)
            .ok_or_else(|| Status::invalid_argument("unknown_surface"))?;
        let respond = |d: &guard::Decision, attempts: Vec<pb::Usage>| {
            Response::new(pb::GuardInputResponse {
                allowed: d.allowed,
                reason_code: d.reason.map(|r| r.code().to_owned()).unwrap_or_default(),
                source: d.source.code().to_owned(),
                guard_version: guard::VERSION.into(),
                attempts,
            })
        };
        // The deterministic pre-filter decides without a model or promotion evidence.
        if let Some(r) = guard::pre_filter(surface, &req.text) {
            return Ok(respond(
                &guard::Decision::block(r, Source::PreFilter),
                vec![],
            ));
        }
        // A live route classifies only with passing live `input_guard` evidence; without it the
        // surface's failure rule decides, as for an outage.
        let mut attempts = vec![];
        let verdict = if self.live_model("GuardInput", "input_guard").is_ok() {
            match guard::classifier::run(&self.router, surface, &req.text, &req.course_topic).await
            {
                Ok(o) => {
                    attempts = o.attempts.iter().map(attempt_usage).collect();
                    o.completion
                        .and_then(|c| guard::parse_verdict(&c.text).ok())
                }
                Err(_) => None,
            }
        } else {
            None
        };
        Ok(respond(
            &guard::Decision::from_verdict(surface, verdict),
            attempts,
        ))
    }
    async fn screen_character(
        &self,
        request: Request<pb::ScreenCharacterRequest>,
    ) -> Result<Response<pb::ScreenCharacterResponse>, Status> {
        let req = request.into_inner();
        if (req.raw_prompt.is_empty() && !engine_core::characters::valid_spec(&req.spec_json))
            || (!req.raw_prompt.is_empty() && !req.spec_json.is_empty())
            || (req.publication_export
                && !engine_core::characters::publication_input_valid(
                    &req.spec_json,
                    &req.compiled_appearance,
                    &req.raw_prompt,
                ))
        {
            return Ok(Response::new(pb::ScreenCharacterResponse {
                approved: false,
                reason_code: "invalid_spec".into(),
                policy_version: "character-v1".into(),
                attempts: vec![],
                publication_screened: false,
            }));
        }
        // ADR 0011 / E10-06: missing live feature evidence keeps writes closed.
        self.live_model("ScreenCharacter", "character_screen")?;
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
            publication_screened: req.publication_export,
        }))
    }
    async fn screen_course(
        &self,
        request: Request<pb::ScreenCourseRequest>,
    ) -> Result<Response<pb::ScreenCourseResponse>, Status> {
        let req = request.into_inner();
        let b = req
            .limits
            .ok_or_else(|| Status::invalid_argument("invalid_limits"))?;
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
        // ADR 0011 / E10-06: a live route screens only with passing live evidence.
        self.live_model("ScreenCourse", "course_screen")?;
        engine_core::sharing::check_request(&req.sample_json, req.sampled_lessons, &limits)
            .map_err(|_| Status::invalid_argument("invalid_request"))?;
        let outcome = engine_core::sharing::screen_course(
            &self.router,
            &req.sample_json,
            req.sampled_lessons,
            &limits,
        )
        .await
        .map_err(|_| Status::unavailable("screening_unavailable"))?;
        let (verdict, reason_codes) = outcome
            .completion
            .as_ref()
            .and_then(|c| engine_core::sharing::verdict(&c.text))
            .ok_or_else(|| Status::unavailable("screening_unavailable"))?;
        Ok(Response::new(pb::ScreenCourseResponse {
            verdict,
            reason_codes,
            policy_version: "course-v1".into(),
            attempts: outcome.attempts.iter().map(attempt_usage).collect(),
        }))
    }
    async fn screen_review(
        &self,
        request: Request<pb::ScreenReviewRequest>,
    ) -> Result<Response<pb::ScreenReviewResponse>, Status> {
        let req = request.into_inner();
        let b = req
            .limits
            .ok_or_else(|| Status::invalid_argument("invalid_limits"))?;
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
        // ADR 0011 / E10-06: a live route screens only with passing live evidence.
        self.live_model("ScreenReview", "screen_review")?;
        let outcome = engine_core::sharing::screen_review(&self.router, &req.text, &limits)
            .await
            .map_err(|_| Status::unavailable("screening_unavailable"))?;
        let (verdict, reason_codes) = outcome
            .completion
            .as_ref()
            .and_then(|c| engine_core::sharing::verdict(&c.text))
            .unwrap_or(("pending_screening".into(), vec![]));
        Ok(Response::new(pb::ScreenReviewResponse {
            verdict,
            reason_codes,
            policy_version: "review-v1".into(),
            attempts: outcome.attempts.iter().map(attempt_usage).collect(),
        }))
    }
    async fn transcribe(
        &self,
        request: Request<pb::TranscribeRequest>,
    ) -> Result<Response<pb::TranscribeResponse>, Status> {
        use engine_core::transcribe as t;
        let req = request.into_inner();
        let recording = t::Recording {
            audio: &req.audio,
            container: &req.container,
            duration_ms: req.duration_ms,
            language_hint: &req.language_hint,
        };
        // Refuse a bad recording before the promotion check or any provider call.
        t::validate(&recording).map_err(transcribe_status)?;
        // Task `transcribe`: a live route transcribes only with passing live evidence.
        self.live_model("Transcribe", "transcribe")?;
        let purpose = match req.purpose.as_str() {
            "tutor" | "note" => req.purpose.as_str(),
            _ => "other",
        };
        let outcome = t::transcribe(&self.router, &recording).await;
        // Only sizes, ids, latency and cost are logged: never audio, never the transcript.
        match &outcome {
            Ok(o) => tracing::info!(
                event = "transcribe",
                purpose,
                audio_bytes = req.audio.len(),
                duration_ms = req.duration_ms,
                model = %o.transcription.model,
                fallback_used = o.fallback_used,
                attempts = o.attempts,
                latency_ms = o.latency_ms,
                cost_nanos = o.transcription.cost_nanos,
                empty = o.transcription.text.is_empty(),
                "transcription finished"
            ),
            Err(e) => tracing::warn!(
                event = "transcribe",
                purpose,
                audio_bytes = req.audio.len(),
                duration_ms = req.duration_ms,
                reason = e.reason(),
                "transcription failed"
            ),
        }
        let o = outcome.map_err(transcribe_status)?;
        let billed_ms = (o.transcription.seconds * 1000.0).round();
        Ok(Response::new(pb::TranscribeResponse {
            text: o.transcription.text,
            language: o.transcription.language,
            duration_ms: if billed_ms.is_finite() && billed_ms > 0.0 {
                billed_ms.min(f64::from(u32::MAX)) as u32
            } else {
                req.duration_ms
            },
            model: o.transcription.model,
            fallback_used: o.fallback_used,
            latency_ms: o.latency_ms,
            cost_nanos: o.transcription.cost_nanos,
        }))
    }
    async fn map_character(
        &self,
        request: Request<pb::MapCharacterRequest>,
    ) -> Result<Response<pb::MapCharacterResponse>, Status> {
        let req = request.into_inner();
        // E10-06: no env switch can promote this feature; only live character_create evidence.
        let task = match req.draft_schema_version {
            0 | 1 => "character_create",
            2 => "character_spec",
            _ => {
                return Err(Status::invalid_argument(
                    "unsupported Character draft version",
                ));
            }
        };
        self.live_model("MapCharacter", task)?;
        let b = req
            .limits
            .ok_or_else(|| Status::invalid_argument("quote bounds required"))?;
        let route = self
            .router
            .models
            .tasks
            .get("character_create")
            .ok_or_else(|| Status::failed_precondition("route unavailable"))?;
        if b.provider != self.router.primary.name()
            || b.fallback_provider != self.router.fallback.name()
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
        if req.draft_schema_version == 2 {
            let (out, attributes) = engine_core::characters::attributes::map_attributes(
                &self.router,
                &req.prompt,
                &limits,
            )
            .await
            .map_err(|_| Status::unavailable("provider_unavailable"))?;
            return Ok(Response::new(pb::MapCharacterResponse {
                attributes_json: if attributes.complete {
                    attributes.json()
                } else {
                    String::new()
                },
                partial_attributes_json: if !attributes.complete && !attributes.values.is_empty() {
                    attributes.json()
                } else {
                    String::new()
                },
                attempts: out.attempts.iter().map(attempt_usage).collect(),
                ..Default::default()
            }));
        }
        let out = engine_core::characters::map_character(&self.router, &req.prompt, &limits)
            .await
            .map_err(|_| Status::unavailable("provider_unavailable"))?;
        Ok(Response::new(pb::MapCharacterResponse {
            spec_json: out.completion.map(|c| c.text).unwrap_or_default(),
            attempts: out.attempts.iter().map(attempt_usage).collect(),
            ..Default::default()
        }))
    }
    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&request).request_id, trace_id = %correlation(&request).trace_id, parent_span_id = %correlation(&request).parent_span_id, span_id = %span_id()))]
    async fn replan_outline(
        &self,
        request: Request<pb::ReplanOutlineRequest>,
    ) -> Result<Response<pb::ReplanOutlineResponse>, Status> {
        use engine_core::{
            courses::outline::{Lesson, Unit},
            steering::{Instruction, replan},
        };
        let req = request.into_inner();
        self.live_model("ReplanOutline", "outline_replan")?;
        let b = req
            .limits
            .ok_or_else(|| Status::invalid_argument("quote required"))?;
        let route = self
            .router
            .models
            .tasks
            .get("outline_replan")
            .ok_or_else(|| Status::failed_precondition("route unavailable"))?;
        if b.model != route.model
            || b.fallback_model != route.fallback_model
            || b.provider != self.router.primary.name()
            || b.fallback_provider != self.router.fallback.name()
            || b.deadline_unix_ms == 0
        {
            return Err(Status::failed_precondition(
                "quote differs from evaluated route",
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
        let mut outline = req
            .outline
            .ok_or_else(|| Status::invalid_argument("outline required"))?;
        let base: Vec<Unit> = outline
            .units
            .iter()
            .map(|u| Unit {
                id: u.unit_id.clone(),
                title: u.title.clone(),
                lessons: u
                    .lessons
                    .iter()
                    .map(|l| Lesson {
                        id: l.lesson_id.clone(),
                        title: l.title.clone(),
                        goal: l.goal.clone(),
                    })
                    .collect(),
            })
            .collect();
        let instructions: Vec<Instruction> = req
            .instructions
            .into_iter()
            .map(|i| Instruction {
                id: i.id,
                text: i.text,
                scope: i.scope,
                added_at_unix_ms: i.added_at_unix_ms,
                version: i.version,
            })
            .collect();
        let result = replan::propose(
            &self.router,
            &base,
            &req.protected_lesson_ids,
            &instructions,
            &limits,
        )
        .await
        .map_err(|_| Status::invalid_argument("invalid replan input or quote"))?;
        let units = result
            .completion
            .as_ref()
            .and_then(|c| replan::parse(&base, &req.protected_lesson_ids, &c.text).ok());
        if let Some(units) = units {
            outline.units = units
                .into_iter()
                .map(|u| pb::Unit {
                    unit_id: u.id,
                    title: u.title,
                    lessons: u
                        .lessons
                        .into_iter()
                        .map(|l| pb::LessonRef {
                            lesson_id: l.id,
                            title: l.title,
                            goal: l.goal,
                        })
                        .collect(),
                })
                .collect();
        }
        Ok(Response::new(pb::ReplanOutlineResponse {
            outline: result.completion.as_ref().map(|_| outline),
            attempts: result.attempts.iter().map(attempt_usage).collect(),
            unavailable: result.completion.is_none(),
        }))
    }
    async fn outline_chat_change(
        &self,
        request: Request<pb::OutlineChatChangeRequest>,
    ) -> Result<Response<pb::OutlineChatChangeResponse>, Status> {
        let req = request.into_inner();
        self.live_model("OutlineChatChange", "outline_chat_change")?;
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
    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&request).request_id, trace_id = %correlation(&request).trace_id, parent_span_id = %correlation(&request).parent_span_id, span_id = %span_id()))]
    async fn synthesize_narration(
        &self,
        request: Request<pb::SynthesizeNarrationRequest>,
    ) -> Result<Response<pb::SynthesizeNarrationResponse>, Status> {
        use engine_core::narration as speech;
        let req = request.into_inner();
        // E17-08: a pinned-speaker segment resolves its Character voice key only through the approved
        // config (retired or unknown keys use the default) and runs under the stricter budgets.
        let character = !req.speaker_key.is_empty();
        let (voice_key, voice, fallback) = if character {
            let r = engine_core::characters::narrate::resolve_voice(&req.voice_key);
            // The provider decides which approved voices it can serve (live: only its configured default).
            match self.speech.voice(&r.voice_key) {
                Some(v) => (r.voice_key, v, r.fallback),
                None => {
                    let v = self
                        .speech
                        .voice("default")
                        .ok_or_else(|| Status::failed_precondition("voice unavailable"))?;
                    ("default".to_string(), v, Some("voice_unavailable"))
                }
            }
        } else {
            let v = self
                .speech
                .voice(&req.voice_key)
                .ok_or_else(|| Status::failed_precondition("voice unavailable"))?;
            (req.voice_key.clone(), v, None)
        };
        let limits = req.limits.unwrap_or_default();
        if !limits.model.is_empty() && limits.model != voice.model_id {
            return Err(Status::failed_precondition(
                "quote route differs from evaluated route",
            ));
        }
        let mut response = pb::SynthesizeNarrationResponse {
            voice_id: voice.voice_id.clone(),
            model_id: voice.model_id.clone(),
            settings_fingerprint: voice.fingerprint(),
            voice_key,
            voice_config_version: engine_core::characters::narrate::voices().version().into(),
            voice_fallback_reason: fallback.unwrap_or_default().into(),
            segment_id: req.segment_id.clone(),
            speaker_key: req.speaker_key.clone(),
            ..Default::default()
        };
        if req.resolve_only {
            return Ok(Response::new(response));
        }
        // E10-06: a live voice vendor synthesizes only with passing live narration evidence.
        self.live_voice("SynthesizeNarration")?;
        let speech_request = speech::SpeechRequest {
            text: req.text,
            language: req.language,
            voice_id: voice.voice_id,
            model_id: voice.model_id.clone(),
        };
        // E14 launch limits: 60 s per call, one repair/retry after the primary, never above the quote.
        // Character segments take the stricter 30 s ceiling.
        let (timeout_ms, attempts) = if character {
            engine_core::characters::narrate::budget(limits.timeout_ms, limits.max_attempts)
        } else {
            (
                match limits.timeout_ms {
                    0 => speech::MAX_TTS_TIMEOUT_MS,
                    t => t.min(speech::MAX_TTS_TIMEOUT_MS),
                },
                match limits.max_attempts {
                    0 => speech::MAX_TTS_ATTEMPTS,
                    a => a.min(speech::MAX_TTS_ATTEMPTS),
                },
            )
        };
        let started = std::time::Instant::now();
        let (out, attempt) = speech::synthesize_bounded(
            self.speech.as_ref(),
            &speech_request,
            std::time::Duration::from_millis(u64::from(timeout_ms)),
            attempts,
        )
        .await
        .map_err(|e| match e {
            speech::SpeechError::MissingText => Status::invalid_argument("narration text missing"),
            speech::SpeechError::Timeout => Status::deadline_exceeded("speech call timed out"),
            speech::SpeechError::InvalidAlignment(_) => Status::data_loss("invalid alignment"),
            speech::SpeechError::RateLimited => {
                Status::resource_exhausted("speech provider rate limited")
            }
            speech::SpeechError::Provider(_) => Status::unavailable("speech provider unavailable"),
        })?;
        let characters = out.alignment.characters.len() as u32;
        let latency_ms = started.elapsed().as_millis().min(u128::from(u32::MAX)) as u32;
        let u = speech::usage_for(&voice.model_id, characters, attempt, latency_ms);
        response.audio_bytes = out.audio.len() as u32;
        response.audio = out.audio;
        response.audio_format = out.format;
        response.duration_ms = out.duration_ms;
        response.characters = characters;
        response.alignment = Some(pb::NarrationAlignment {
            characters: out.alignment.characters,
            start_ms: out.alignment.start_ms,
            end_ms: out.alignment.end_ms,
        });
        response.usage = Some(pb::Usage {
            provider: u.provider,
            model: u.model,
            component: u.component,
            narration_characters: u.narration_characters,
            attempt: u.attempt,
            latency_ms: u.latency_ms,
            delivered: u.delivered,
            ..Default::default()
        });
        Ok(Response::new(response))
    }
    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&_request).request_id, trace_id = %correlation(&_request).trace_id, parent_span_id = %correlation(&_request).parent_span_id, span_id = %span_id()))]
    async fn health(
        &self,
        _request: Request<pb::HealthRequest>,
    ) -> Result<Response<pb::HealthResponse>, Status> {
        // The first two entries stay the primary and fallback provider names; one
        // `route:<task>=<provider>/<model>,<provider>/<fallback model>` entry per configured
        // task follows, the identity a quote must name (same in mock and live mode).
        let mut providers = vec![
            self.router.primary.name().to_string(),
            self.router.fallback.name().to_string(),
        ];
        // Transport identity, separate from the quote identity: fixture routers report the live
        // route names but never call a model, screen or check promotion evidence.
        let mode = if self.router.fixture_only() {
            "mode:fixture"
        } else {
            "mode:live"
        };
        providers.push(mode.to_string());
        providers.extend(self.router.models.tasks.iter().map(|(task, m)| {
            format!(
                "route:{task}={}/{},{}/{}",
                self.router.primary.name(),
                m.model,
                self.router.fallback.name(),
                m.fallback_model
            )
        }));
        Ok(Response::new(pb::HealthResponse {
            version: env!("CARGO_PKG_VERSION").into(),
            providers,
        }))
    }
    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&request).request_id, trace_id = %correlation(&request).trace_id, parent_span_id = %correlation(&request).parent_span_id, span_id = %span_id()))]
    async fn preview_course(
        &self,
        request: Request<pb::PreviewCourseRequest>,
    ) -> Result<Response<pb::PreviewCourseResponse>, Status> {
        let req = request.into_inner();
        self.live_model("PreviewCourse", "course_preview")?;
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
        let mut outcome = engine_core::credits::run_validated_checked(
            &self.router,
            "preview",
            &prompt,
            &limits,
            engine_core::courses::check_preview,
        )
        .await
        .map_err(|_| Status::invalid_argument("invalid API envelope"))?;
        let parsed = outcome
            .completion
            .as_ref()
            .and_then(|c| Some((outline(c).ok()?, slides(c, false).ok()?)));
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
        self.live_model("GenerateCourseOutline", "course_outline")?;
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
        let mut req = request.into_inner();
        self.live_model("GenerateLesson", "lesson_slides")?;
        if let Some(b) = req.limits.as_mut()
            && b.deadline_unix_ms == 0
        {
            b.deadline_unix_ms = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map_err(|_| Status::internal("clock unavailable"))?
                .as_millis()
                .saturating_add(u128::from(b.timeout_ms)) as u64;
        }
        let bounds = req
            .limits
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("API quote bounds required"))?;
        let limits = lesson_limits(&self.router, bounds)?;
        let cast = req.character_personas_json.clone();
        let mut context = req.clone();
        context.character_personas_json.clear();
        context.instructions.clear();
        let instructions: Vec<_> = req
            .instructions
            .iter()
            .map(|i| engine_core::steering::Instruction {
                id: i.id.clone(),
                text: i.text.clone(),
                scope: i.scope,
                added_at_unix_ms: i.added_at_unix_ms,
                version: i.version,
            })
            .collect();
        // Validate bounds early; the boundary itself is rebuilt per prompt by hardened_prompt.
        engine_core::steering::prompt_section(&instructions)
            .map_err(|_| Status::invalid_argument("instruction bounds exceeded"))?;
        let context_text = engine_core::lessontest::render_context(&context);
        let routed = engine_core::skills::bundled().mode("lesson_slides")
            == engine_core::skills::PromptMode::Routed;
        let personas = if cast.is_empty() {
            String::new()
        } else {
            engine_core::characters::lesson_personas(&cast)
                .map_err(|_| Status::invalid_argument("invalid character personas"))?
        };
        let neutral = "Use a neutral default narrator style for every speaker_key in character_personas; ignore persona flavour.";
        let mut prompt_info = ("monolithic", "", 0u32);
        let (system, prompts) = if routed {
            let learner = req.learner.as_ref();
            let lesson = req.lesson.as_ref();
            let selection = engine_core::lessontest::select_lesson_skills(
                learner.map_or("", |l| l.locale.as_str()),
                !cast.is_empty(),
                learner.map_or("", |l| l.level.as_str()),
                lesson.map_or("", |l| l.title.as_str()),
                lesson.map_or("", |l| l.goal.as_str()),
                learner.map_or("", |l| l.topic.as_str()),
            )
            .await;
            let ids = selection.ids;
            prompt_info = ("routed", selection.source.code(), ids.len() as u32);
            let p = engine_core::lessontest::routed_lesson_prompt(&context_text, &ids)
                .map_err(|e| Status::internal(e.to_string()))?;
            let prompts = if cast.is_empty() {
                vec![p.user]
            } else {
                // Attempt 2 regenerates with neutral default personas, still pinned keys.
                vec![
                    format!("{}\n{personas}", p.user),
                    format!("{}\n{neutral}", p.user),
                ]
            };
            (Some(p.system), prompts)
        } else {
            let base = engine_core::lessontest::lesson_prompt(&context_text);
            let prompts = if cast.is_empty() {
                vec![base]
            } else {
                let segments = " Optional per-slide segments[] {speaker_key,text,expression,start_ms,duration_ms}: plain text only, speaker_key from character_personas, expression one of idle|happy|sad|thinking|celebrate.";
                vec![
                    format!("{base}{segments}\n{personas}"),
                    format!("{base}{segments}\n{neutral}"),
                ]
            };
            (None, prompts)
        };
        let prompts: Vec<String> = prompts
            .iter()
            .map(|data| engine_core::steering::hardened_prompt(data, &instructions))
            .collect::<anyhow::Result<_>>()
            .map_err(|_| Status::invalid_argument("instruction bounds exceeded"))?;
        let inputs: Vec<&str> = prompts.iter().map(String::as_str).collect();
        // Character lessons add per-slide segments, so only plain lessons take the strict schema.
        let shape = engine_core::eval::Shape {
            format: cast.is_empty().then(engine_core::structured::lesson_format),
            prepare: Some(engine_core::lessontest::prepare_lesson),
            system,
        };
        let outcome = engine_core::eval::run_bounded_formatted(
            &self.router,
            "lesson",
            &inputs,
            &limits,
            shape,
            |text| engine_core::lessontest::check_generated(text, &instructions, &cast),
            true,
        )
        .await
        .map_err(internal)?;
        let mut attempts: Vec<pb::Usage> = outcome.attempts.iter().map(attempt_usage).collect();
        for a in &mut attempts {
            a.prompt_mode = prompt_info.0.into();
            a.router_source = prompt_info.1.into();
            a.skill_count = prompt_info.2;
        }
        let Some(c) = outcome.completion else {
            return Ok(lesson_events(None, attempts));
        };
        let generated_slides = slides(&c, true)?;
        let value = content(&c)?;
        let test_questions: Vec<pb::Question> = serde_json::from_value::<
            Vec<engine_core::lessontest::retry::Question>,
        >(value["test"].clone())
        .map_err(|_| Status::internal("invalid test identities"))?
        .into_iter()
        .map(proto_question)
        .collect();
        let mut variant_pool = Vec::new();
        if let Some(pools) = value["variant_pool"].as_array() {
            for pool in pools {
                variant_pool.push(pb::TopicVariants {
                    topic_id: pool["topic_id"].as_str().unwrap_or_default().into(),
                    variants:
                        serde_json::from_value::<Vec<engine_core::lessontest::retry::Question>>(
                            pool["variants"].clone(),
                        )
                        .map_err(|_| Status::internal("invalid variants"))?
                        .into_iter()
                        .map(proto_question)
                        .collect(),
                });
            }
        }
        for source in &test_questions {
            if variant_pool
                .iter()
                .any(|p| p.topic_id == source.topic_id && !p.variants.is_empty())
            {
                continue;
            }
            let mut remaining = bounds.clone();
            remaining.max_attempts = limits.max_attempts.saturating_sub(attempts.len() as u32);
            let mut spent = 0u64;
            for a in &attempts {
                if a.usage_unavailable {
                    remaining.max_attempts = 0;
                    break;
                }
                spent = spent.saturating_add(
                    engine_core::credits::check_usage(
                        &limits,
                        a.input_tokens,
                        a.output_tokens,
                        a.cache_write_tokens,
                        a.cache_read_tokens,
                    )
                    .map_err(internal)?,
                );
            }
            remaining.max_cost_nanos = limits.max_cost_nanos.saturating_sub(spent);
            if remaining.max_attempts == 0 || remaining.max_cost_nanos == 0 {
                for a in &mut attempts {
                    a.delivered = false;
                }
                return Ok(lesson_events(None, attempts));
            }
            let response = self
                .generate_retry_variants(Request::new(pb::GenerateRetryVariantsRequest {
                    source: Some(source.clone()),
                    previous_variants: vec![],
                    learner: req.learner.clone(),
                    limits: Some(remaining),
                    character_personas_json: cast.clone(),
                    instructions: req.instructions.clone(),
                }))
                .await?
                .into_inner();
            let offset = attempts.len() as u32;
            for mut a in response.attempts {
                a.attempt += offset;
                attempts.push(a);
            }
            if response.unavailable {
                for a in &mut attempts {
                    a.delivered = false;
                }
                return Ok(lesson_events(None, attempts));
            }
            variant_pool.retain(|p| p.topic_id != source.topic_id);
            variant_pool.push(pb::TopicVariants {
                topic_id: source.topic_id.clone(),
                variants: response.variants,
            });
        }
        Ok(lesson_events(
            Some(pb::Lesson {
                applied_instruction_ids: req.instructions.iter().map(|i| i.id.clone()).collect(),
                r#ref: req.lesson,
                slides: generated_slides,
                test: test_questions,
                variant_pool,
            }),
            attempts,
        ))
    }
    #[tracing::instrument(name = "engine.rpc", skip_all)]
    async fn generate_retry_variants(
        &self,
        request: Request<pb::GenerateRetryVariantsRequest>,
    ) -> Result<Response<pb::GenerateRetryVariantsResponse>, Status> {
        let req = request.into_inner();
        self.live_model("GenerateRetryVariants", "lesson_test")?;
        // Persona data never reaches grading prompts; it must still be valid.
        if !req.character_personas_json.is_empty() {
            engine_core::characters::lesson_personas(&req.character_personas_json)
                .map_err(|_| Status::invalid_argument("invalid character personas"))?;
        }
        let source = req
            .source
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("source question required"))?;
        let bounds = req
            .limits
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("API quote bounds required"))?;
        let limits = lesson_limits(&self.router, bounds)?;
        let source = retry_question(source);
        let previous: Vec<_> = req.previous_variants.iter().map(retry_question).collect();
        let instructions: Vec<_> = req
            .instructions
            .iter()
            .map(|i| engine_core::steering::Instruction {
                id: i.id.clone(),
                text: i.text.clone(),
                scope: i.scope,
                added_at_unix_ms: i.added_at_unix_ms,
                version: i.version,
            })
            .collect();
        let outcome = engine_core::lessontest::retry::generate_with_instructions(
            &self.router,
            &source,
            &previous,
            &limits,
            &instructions,
        )
        .await
        .map_err(internal)?;
        let variants = match &outcome.completion {
            Some(c) => engine_core::lessontest::retry::parse_variants(&source, &previous, &c.text)
                .map_err(internal)?
                .into_iter()
                .map(proto_question)
                .collect(),
            None => vec![],
        };
        Ok(Response::new(pb::GenerateRetryVariantsResponse {
            variants,
            usage: None,
            attempts: outcome.attempts.iter().map(attempt_usage).collect(),
            unavailable: outcome.completion.is_none(),
        }))
    }
    type ChatStream = RpcStream<pb::ChatEvent>;
    #[tracing::instrument(name = "engine.rpc", skip_all, fields(request_id = %correlation(&request).request_id, trace_id = %correlation(&request).trace_id, parent_span_id = %correlation(&request).parent_span_id, span_id = %span_id()))]
    async fn chat(
        &self,
        request: Request<pb::ChatRequest>,
    ) -> Result<Response<Self::ChatStream>, Status> {
        let req = request.into_inner();
        self.live_model("Chat", "tutor_chat")?;
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
        let limits = req.limits.as_ref().map(|b| engine_core::credits::Limits {
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
        });
        if let Some(b) = &req.limits
            && let Some(model) = self.router.models.tasks.get("chat")
            && ((!b.provider.is_empty() && b.provider != self.router.primary.name())
                || (!b.model.is_empty() && b.model != model.model)
                || (!b.fallback_provider.is_empty()
                    && b.fallback_provider != self.router.fallback.name())
                || (!b.fallback_model.is_empty() && b.fallback_model != model.fallback_model))
        {
            return Err(Status::failed_precondition(
                "quote route differs from evaluated route",
            ));
        }
        let input = engine_core::chat::prompt(&tutor_context(&req));
        let span = tracing::Span::current();
        let (stream, route) = engine_core::eval::stream_bounded_routed(
            &self.router,
            "chat",
            &input,
            limits.as_ref(),
            first_to,
            idle_to,
        )
        .await
        .map_err(internal)?;
        let bounds = limits.as_ref().map(|l| engine_core::chat::stream::Bounds {
            input_tokens: l.input_tokens,
            output_tokens: l.output_tokens,
        });
        let parts = engine_core::chat::stream::reconcile(stream, route, input.len(), bounds);
        // Enter the request span while polling the provider as well as mapping
        // its output; a map closure alone runs after provider work has finished.
        let map_span = span.clone();
        let finished = CancelLog::new(span.clone());
        let stream = futures::stream::unfold(
            (Box::pin(parts), span, finished),
            |(mut parts, span, mut finished)| async move {
                let next = parts.next().instrument(span.clone()).await;
                if next.is_none() {
                    finished.complete();
                }
                next.map(|part| (part, (parts, span, finished)))
            },
        )
        .map(move |part| {
            use engine_core::chat::stream::Part;
            map_span.in_scope(|| {
                Ok(pb::ChatEvent {
                    event: Some(match part {
                        Part::Delta(delta) => pb::chat_event::Event::Delta(delta),
                        Part::Usage {
                            completion,
                            estimated,
                        } => pb::chat_event::Event::Usage(
                            usage_estimated(&completion, estimated).expect("usage"),
                        ),
                        Part::Error(failure) => {
                            pb::chat_event::Event::StreamError(pb::ChatStreamError {
                                code: failure.code.into(),
                                retryable: failure.retryable,
                            })
                        }
                    }),
                })
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
        self.live_model("EnrichNote", "note_enrich")?;
        let route = self
            .router
            .models
            .tasks
            .get("note")
            .ok_or_else(|| Status::failed_precondition("route unavailable"))?;
        if let Some(b) = &req.limits
            && ((!b.provider.is_empty() && b.provider != self.router.primary.name())
                || (!b.model.is_empty() && b.model != route.model)
                || (!b.fallback_provider.is_empty()
                    && b.fallback_provider != self.router.fallback.name())
                || (!b.fallback_model.is_empty() && b.fallback_model != route.fallback_model))
        {
            return Err(Status::failed_precondition(
                "quote route differs from evaluated route",
            ));
        }

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
        self.live_model("CourseBriefChat", "course_brief_chat")?;
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
        self.live_model("SetupChat", "setup_chat")?;
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
                model: self
                    .router
                    .models
                    .tasks
                    .get("setup_chat")
                    .map(|m| m.model.clone())
                    .unwrap_or_default(),
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

/// Default time in-flight RPCs get to finish after SIGTERM (`ENGINE_DRAIN_TIMEOUT_SECS` overrides it).
/// It exceeds the longest quoted preview/lesson budget; compose `stop_grace_period` must stay above it.
pub const DEFAULT_DRAIN_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(90);

/// Reads `ENGINE_DRAIN_TIMEOUT_SECS`; empty, zero or invalid keeps [`DEFAULT_DRAIN_TIMEOUT`].
pub fn drain_timeout_from(value: Option<&str>) -> std::time::Duration {
    value
        .and_then(|v| v.trim().parse::<u64>().ok())
        .filter(|s| *s > 0)
        .map(std::time::Duration::from_secs)
        .unwrap_or(DEFAULT_DRAIN_TIMEOUT)
}

/// Runs a tonic server until `signal` resolves, then drains: the server stops accepting connections and
/// lets in-flight RPCs finish for up to `drain`; after that the server future is dropped, which closes
/// whatever is left. `build` receives the shutdown future to hand to `serve_with_shutdown`.
pub async fn serve_with_drain<B, Fut, S>(
    build: B,
    signal: S,
    drain: std::time::Duration,
) -> Result<(), tonic::transport::Error>
where
    B: FnOnce(std::pin::Pin<Box<dyn std::future::Future<Output = ()> + Send>>) -> Fut,
    Fut: std::future::Future<Output = Result<(), tonic::transport::Error>>,
    S: std::future::Future<Output = ()>,
{
    let (stop, stopped) = tokio::sync::oneshot::channel::<()>();
    let server = build(Box::pin(async move {
        let _ = stopped.await;
    }));
    tokio::pin!(server);
    let deadline = async move {
        signal.await;
        tracing::info!(drain_secs = drain.as_secs_f64(), "draining in-flight RPCs");
        let _ = stop.send(());
        tokio::time::sleep(drain).await;
    };
    tokio::select! {
        result = &mut server => result,
        () = deadline => {
            tracing::warn!("drain timeout reached; closing remaining RPCs");
            Ok(())
        }
    }
}
