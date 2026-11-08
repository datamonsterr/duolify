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
}
