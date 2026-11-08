use engine_core::{Completion, ProviderRouter};
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
        units: vec![],
    })
}
fn slide(c: &Completion) -> Result<pb::Slide, Status> {
    let v = content(c)?;
    let html = v["html"]
        .as_str()
        .ok_or_else(|| Status::internal("missing slide html"))?;
    engine_core::slide::validate(html, &[]).map_err(|_| Status::internal("invalid slide"))?;
    Ok(pb::Slide {
        slide_id: String::new(),
        html: html.into(),
        duration_ms: 1000,
        narration: String::new(),
    })
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
            providers: vec!["configured".into()],
        }))
    }
    async fn preview_course(
        &self,
        request: Request<pb::PreviewCourseRequest>,
    ) -> Result<Response<pb::PreviewCourseResponse>, Status> {
        let req = request.into_inner();
        let c = self
            .router
            .complete("preview", &req.topic)
            .await
            .map_err(internal)?;
        Ok(Response::new(pb::PreviewCourseResponse {
            sample_outline: Some(outline(&c)?),
            example_slides: vec![slide(&c)?],
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
            .complete("outline", &req.topic)
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
        let s = slide(&c)?;
        let l = pb::Lesson {
            r#ref: req.lesson,
            slides: vec![s.clone()],
            test: vec![],
        };
        let events = vec![
            pb::generate_lesson_event::Event::Progress("Generating".into()),
            pb::generate_lesson_event::Event::Slide(s),
            pb::generate_lesson_event::Event::Lesson(l),
            pb::generate_lesson_event::Event::Usage(usage(&c).unwrap()),
        ];
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
                r.map(|delta| pb::ChatEvent {
                    event: Some(pb::chat_event::Event::Delta(delta)),
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
            .complete("note", &req.note)
            .await
            .map_err(internal)?;
        Ok(Response::new(pb::EnrichNoteResponse {
            summary: content(&c)?["summary"]
                .as_str()
                .ok_or_else(|| Status::internal("missing summary"))?
                .into(),
            tags: vec![],
            links: vec![],
            usage: usage(&c),
        }))
    }
}
