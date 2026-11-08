use engine_proto::{
    self as pb, engine_service_client::EngineServiceClient,
    engine_service_server::EngineServiceServer,
};
use engine_server::Service;
use std::sync::Arc;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;
#[tokio::test]
async fn all_rpcs() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    let server = EngineServiceServer::new(Service {
        router: Arc::new(engine_providers::router("mock").unwrap()),
    });
    let task = tokio::spawn(async move {
        Server::builder()
            .add_service(server)
            .serve_with_incoming(TcpListenerStream::new(listener))
            .await
            .unwrap();
    });
    let mut c = EngineServiceClient::connect(format!("http://{addr}"))
        .await
        .unwrap();
    assert!(
        !c.health(pb::HealthRequest {})
            .await
            .unwrap()
            .into_inner()
            .version
            .is_empty()
    );
    let preview = c
        .preview_course(pb::PreviewCourseRequest {
            topic: "test".into(),
            learner: None,
        })
        .await
        .unwrap()
        .into_inner();
    assert!(!preview.example_slides.is_empty());
    assert!(!preview.sample_outline.unwrap().units.is_empty());
    assert!(
        c.generate_course_outline(pb::GenerateCourseOutlineRequest {
            topic: "test".into(),
            learner: None
        })
        .await
        .unwrap()
        .into_inner()
        .outline
        .is_some()
    );
    let mut lesson = c
        .generate_lesson(pb::GenerateLessonRequest::default())
        .await
        .unwrap()
        .into_inner();
    let mut n = 0;
    while lesson.message().await.unwrap().is_some() {
        n += 1;
    }
    assert!(n >= 3);
    let mut chat = c
        .chat(pb::ChatRequest::default())
        .await
        .unwrap()
        .into_inner();
    assert!(matches!(
        chat.message().await.unwrap().unwrap().event,
        Some(pb::chat_event::Event::Delta(_))
    ));
    assert!(matches!(
        chat.message().await.unwrap().unwrap().event,
        Some(pb::chat_event::Event::Usage(_))
    ));
    assert!(
        !c.enrich_note(pb::EnrichNoteRequest::default())
            .await
            .unwrap()
            .into_inner()
            .summary
            .is_empty()
    );
    task.abort();
}
