use super::*;
use std::sync::atomic::{AtomicUsize, Ordering};

fn lib() -> Library {
    Library::bundled().unwrap()
}

struct Fixed(BTreeMap<String, f64>, AtomicUsize);
#[async_trait]
impl SkillRouter for Fixed {
    async fn decide(
        &self,
        _: &RouterInput,
        _: &[Candidate],
    ) -> anyhow::Result<BTreeMap<String, f64>> {
        self.1.fetch_add(1, Ordering::SeqCst);
        Ok(self.0.clone())
    }
}
struct Failing;
#[async_trait]
impl SkillRouter for Failing {
    async fn decide(
        &self,
        _: &RouterInput,
        _: &[Candidate],
    ) -> anyhow::Result<BTreeMap<String, f64>> {
        anyhow::bail!("outage")
    }
}
struct Slow;
#[async_trait]
impl SkillRouter for Slow {
    async fn decide(
        &self,
        _: &RouterInput,
        _: &[Candidate],
    ) -> anyhow::Result<BTreeMap<String, f64>> {
        tokio::time::sleep(Duration::from_secs(5)).await;
        Ok(BTreeMap::new())
    }
}

fn input() -> RouterInput {
    RouterInput {
        task: "lesson_slides".into(),
        title: "How TCP works".into(),
        ..Default::default()
    }
}
const MANAGED: &[&str] = &[
    "narration_pacing",
    "language_practice",
    "character_cast",
    "age_band_tone",
];

async fn run(router: Option<&dyn SkillRouter>, cache: &dyn DecisionCache) -> Selection {
    select(
        &lib(),
        "lesson_slides",
        vec!["narration_pacing".into()],
        MANAGED,
        &BTreeMap::new(),
        &input(),
        router,
        cache,
        &NoBreaker,
        Duration::from_millis(50),
    )
    .await
}

#[tokio::test]
async fn model_picks_only_allowed_skills_above_threshold_most_likely_first() {
    let probs = BTreeMap::from([
        ("timeline".to_string(), 0.95),
        ("diagram_layout".to_string(), 0.99),
        ("code_slide".to_string(), 0.5),
        ("character_cast".to_string(), 0.99), // rule-managed: never model-decided
        ("made_up".to_string(), 1.0),         // unknown key
        ("math_notation".to_string(), f64::NAN),
    ]);
    let r = Fixed(probs, AtomicUsize::new(0));
    let s = run(Some(&r), &MemoryCache::default()).await;
    assert_eq!(s.source, Source::Model);
    assert_eq!(s.ids, ["narration_pacing", "diagram_layout", "timeline"]);
}

#[tokio::test]
async fn second_identical_request_is_served_from_cache_without_the_model() {
    let r = Fixed(
        BTreeMap::from([("code_slide".to_string(), 0.97)]),
        AtomicUsize::new(0),
    );
    let cache = MemoryCache::default();
    let a = run(Some(&r), &cache).await;
    let b = run(Some(&r), &cache).await;
    assert_eq!(a.ids, b.ids);
    assert_eq!((a.source, b.source), (Source::Model, Source::Cache));
    assert_eq!(r.1.load(Ordering::SeqCst), 1);
}

#[tokio::test]
async fn outage_timeout_and_missing_router_use_the_default_set_and_are_not_cached() {
    let cache = MemoryCache::default();
    for r in [Some(&Failing as &dyn SkillRouter), Some(&Slow), None] {
        let s = run(r, &cache).await;
        assert_eq!(s.source, Source::Default);
        assert_eq!(s.ids, ["narration_pacing", "diagram_layout"]);
    }
    let ok = Fixed(BTreeMap::new(), AtomicUsize::new(0));
    // Nothing cached by failures, so the model is consulted now; an empty answer is valid.
    let s = run(Some(&ok), &cache).await;
    assert_eq!((s.source, s.ids.len()), (Source::Model, 1));
}

#[tokio::test]
async fn unknown_task_or_no_candidates_is_rules_only() {
    let s = select(
        &lib(),
        "nope",
        vec!["a".into()],
        &[],
        &BTreeMap::new(),
        &input(),
        None,
        &MemoryCache::default(),
        &NoBreaker,
        TIMEOUT,
    )
    .await;
    assert_eq!((s.source, s.ids), (Source::Rules, vec!["a".to_string()]));
}

#[test]
fn input_is_bounded_and_the_cache_key_tracks_input_and_thresholds() {
    let long = RouterInput {
        title: "t".repeat(900),
        goal: "g".repeat(50),
        ..Default::default()
    }
    .bounded();
    assert_eq!(long.title.chars().count(), MAX_INPUT_CHARS);
    assert!(long.goal.is_empty());
    let c = |t: f64| {
        vec![Candidate {
            id: "timeline".into(),
            description: String::new(),
            threshold: t,
        }]
    };
    let l = lib();
    let k = cache_key(&l, "lesson_slides", &c(0.9), &input());
    assert_eq!(k, cache_key(&l, "lesson_slides", &c(0.9), &input()));
    assert_ne!(k, cache_key(&l, "lesson_slides", &c(0.8), &input()));
    let mut other = input();
    other.title = "other".into();
    assert_ne!(k, cache_key(&l, "lesson_slides", &c(0.9), &other));
}

#[test]
fn breaker_opens_when_router_spend_passes_one_percent_of_engine_spend_above_the_floor() {
    assert!(
        !breaker_open(BREAKER_FLOOR_NANOS, 0),
        "the floor is always allowed"
    );
    assert!(breaker_open(BREAKER_FLOOR_NANOS + 1, 0));
    assert!(!breaker_open(2_000_000, 200_000_000));
    assert!(breaker_open(2_000_000, 199_999_999));
    assert!(
        !breaker_open(u64::MAX, u64::MAX),
        "saturating, no overflow panic"
    );
}

#[tokio::test]
async fn open_breaker_skips_the_model_and_uses_the_default_set() {
    let r = Fixed(
        BTreeMap::from([("code_slide".to_string(), 0.99)]),
        AtomicUsize::new(0),
    );
    let b = MemoryBreaker::default();
    b.record_router(BREAKER_FLOOR_NANOS + 1).await;
    let s = select(
        &lib(),
        "lesson_slides",
        vec!["narration_pacing".into()],
        MANAGED,
        &BTreeMap::new(),
        &input(),
        Some(&r),
        &MemoryCache::default(),
        &b,
        Duration::from_millis(50),
    )
    .await;
    assert_eq!(s.source, Source::Breaker);
    assert_eq!(s.ids, ["narration_pacing", "diagram_layout"]);
    assert_eq!(r.1.load(Ordering::SeqCst), 0);
    // Main-model spend reopens it.
    b.record_engine(10_000_000_000).await;
    assert!(!b.open().await);
}

#[tokio::test]
async fn each_model_call_is_counted_and_a_cache_hit_is_free() {
    let r = Fixed(BTreeMap::new(), AtomicUsize::new(0));
    let b = MemoryBreaker::default();
    let cache = MemoryCache::default();
    for _ in 0..2 {
        select(
            &lib(),
            "lesson_slides",
            vec!["narration_pacing".into()],
            MANAGED,
            &BTreeMap::new(),
            &input(),
            Some(&r),
            &cache,
            &b,
            Duration::from_millis(50),
        )
        .await;
    }
    assert_eq!(r.1.load(Ordering::SeqCst), 1);
    assert_eq!(b.router.load(Ordering::Relaxed), ROUTER_CALL_NANOS);
}

/// The decision model sees only these descriptions, so overlapping ones make it over-pick
/// (CI live A/B: `timeline` fired on every process and cycle, `diagram_layout` on comparisons).
#[test]
fn model_routed_descriptions_are_disjoint_in_scope() {
    let lib = lib();
    let ts = lib.task("lesson_slides").unwrap();
    let d = |id: &str| ts.skills[id].description.to_lowercase();
    let (diagram, timeline, table) = (d("diagram_layout"), d("timeline"), d("comparison_table"));
    for cue in ["cycle", "steps", "flow", "how something works"] {
        assert!(diagram.contains(cue), "diagram_layout should name {cue}");
    }
    assert!(
        timeline.contains("dated") && timeline.contains("not"),
        "timeline must require dated, chronological events and exclude processes"
    );
    assert!(
        !timeline.contains("stages"),
        "stages/sequence wording pulls processes into timeline"
    );
    assert!(diagram.contains("not") && table.contains("not"));
}
