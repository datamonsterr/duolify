//! Skill router (ADR 0025): picks which optional skills a routed prompt loads. Order: rules
//! (request fields the engine already holds), then the decision cache, then a decision model,
//! then the task's default set. The model only answers yes or no per allowed skill id; it sees
//! `RouterInput` (short fields, no learner free text), never blocks generation (timeout and
//! every failure fall back to the default set) and cannot name a skill outside the allowed set.
use crate::skills::Library;
use async_trait::async_trait;
use serde::Serialize;
use std::{
    collections::{BTreeMap, HashMap},
    sync::Mutex,
    time::Duration,
};

/// A skill is loaded when the model's probability for it reaches this value (D7), unless the
/// caller passes a per-skill threshold.
pub const DEFAULT_THRESHOLD: f64 = 0.9;
/// The router must answer inside this time or the default set is used (spec: 800 ms).
pub const TIMEOUT: Duration = Duration::from_millis(800);
/// Bumped when the question wording or selection logic changes; part of the cache key.
pub const ROUTER_VERSION: u32 = 1;
/// All `RouterInput` text together is cut to this many characters (D6).
pub const MAX_INPUT_CHARS: usize = 600;

/// What the decision model may see. Titles and goals come from the course outline; chat,
/// notes and profile free text are not part of it (type-level: these are the only fields).
#[derive(Debug, Clone, Default, Serialize, PartialEq, Eq)]
pub struct RouterInput {
    pub task: String,
    pub title: String,
    pub goal: String,
    pub topic: String,
    pub level: String,
    pub locale: String,
    pub flags: Vec<String>,
}

impl RouterInput {
    /// The same input cut to `MAX_INPUT_CHARS` of text, long fields first.
    pub fn bounded(mut self) -> Self {
        let mut left = MAX_INPUT_CHARS;
        for f in [
            &mut self.title,
            &mut self.goal,
            &mut self.topic,
            &mut self.level,
            &mut self.locale,
        ] {
            let kept: String = f.chars().take(left).collect();
            left -= kept.chars().count();
            *f = kept;
        }
        self
    }
}

/// One skill the model may decide on.
#[derive(Debug, Clone, PartialEq)]
pub struct Candidate {
    pub id: String,
    pub description: String,
    pub threshold: f64,
}

/// A decision model: probability of yes per candidate id. Extra ids in the answer are ignored.
#[async_trait]
pub trait SkillRouter: Send + Sync {
    async fn decide(
        &self,
        input: &RouterInput,
        candidates: &[Candidate],
    ) -> anyhow::Result<BTreeMap<String, f64>>;

    /// Picks ONE of `options` (an exclusive group the per-skill pass said yes to): probability per
    /// option id, the highest wins. Default: unsupported, so the caller keeps the first-listed.
    async fn resolve(
        &self,
        _input: &RouterInput,
        _options: &[Candidate],
    ) -> anyhow::Result<BTreeMap<String, f64>> {
        anyhow::bail!("choice not supported")
    }
}

/// Disposable decision cache keyed by `cache_key`; the production backend is Redis.
#[async_trait]
pub trait DecisionCache: Send + Sync {
    async fn get(&self, key: &str) -> Option<Vec<String>>;
    async fn put(&self, key: String, ids: Vec<String>);
}

#[derive(Default)]
pub struct MemoryCache(Mutex<HashMap<String, Vec<String>>>);

#[async_trait]
impl DecisionCache for MemoryCache {
    async fn get(&self, key: &str) -> Option<Vec<String>> {
        self.0.lock().ok()?.get(key).cloned()
    }
    async fn put(&self, key: String, ids: Vec<String>) {
        if let Ok(mut m) = self.0.lock() {
            m.insert(key, ids);
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum Source {
    /// Nothing for a model to decide (no candidate skills).
    Rules,
    Cache,
    Model,
    /// Router missing, slow, failing or answering nonsense: the task's safe default set.
    Default,
    /// The daily budget breaker is open: no model call, the default set.
    Breaker,
}

impl Source {
    /// Wire and analytics name.
    pub fn code(self) -> &'static str {
        match self {
            Source::Rules => "rules",
            Source::Cache => "cache",
            Source::Model => "model",
            Source::Default => "default",
            Source::Breaker => "breaker",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Selection {
    pub ids: Vec<String>,
    pub source: Source,
}

/// Cache key over everything that changes the answer: task, skill set and versions, router
/// version, thresholds and the (bounded) input.
pub fn cache_key(
    lib: &Library,
    task: &str,
    candidates: &[Candidate],
    input: &RouterInput,
) -> String {
    let versions: Vec<String> = lib
        .task(task)
        .map(|t| {
            t.skills
                .values()
                .map(|s| format!("{}:{}", s.id, s.version))
                .collect()
        })
        .unwrap_or_default();
    let thresholds: Vec<String> = candidates
        .iter()
        .map(|c| format!("{}={}", c.id, c.threshold))
        .collect();
    let groups = lib
        .task(task)
        .map(|t| format!("{:?}", t.exclusive))
        .unwrap_or_default();
    let raw = format!(
        "{task}|{}|{ROUTER_VERSION}|{}|{groups}|{}",
        versions.join(","),
        thresholds.join(","),
        serde_json::to_string(input).unwrap_or_default()
    );
    ring::digest::digest(&ring::digest::SHA256, raw.as_bytes())
        .as_ref()
        .iter()
        .map(|b| format!("{b:02x}"))
        .collect()
}

/// Chooses skill ids, in priority order, for `task`. `rule_ids` are decided by rule and always
/// first; `rule_managed` are skills the model must never decide (their rule already ran), so
/// the model's candidates are the task's other skills. Never fails.
#[allow(clippy::too_many_arguments)]
pub async fn select(
    lib: &Library,
    task: &str,
    rule_ids: Vec<String>,
    rule_managed: &[&str],
    thresholds: &BTreeMap<String, f64>,
    input: &RouterInput,
    router: Option<&dyn SkillRouter>,
    cache: &dyn DecisionCache,
    breaker: &dyn Breaker,
    timeout: Duration,
) -> Selection {
    let Some(ts) = lib.task(task) else {
        return Selection {
            ids: rule_ids,
            source: Source::Rules,
        };
    };
    let candidates: Vec<Candidate> = ts
        .skills
        .values()
        .filter(|s| !rule_managed.contains(&s.id.as_str()) && !rule_ids.contains(&s.id))
        .map(|s| Candidate {
            id: s.id.clone(),
            description: s.description.clone(),
            threshold: thresholds.get(&s.id).copied().unwrap_or(DEFAULT_THRESHOLD),
        })
        .collect();
    let with = |extra: Vec<String>| {
        let mut ids = rule_ids.clone();
        ids.extend(extra);
        ids
    };
    if candidates.is_empty() {
        return Selection {
            ids: rule_ids,
            source: Source::Rules,
        };
    }
    let input = input.clone().bounded();
    let key = cache_key(lib, task, &candidates, &input);
    if let Some(hit) = cache.get(&key).await {
        // A stale entry can only name skills that still exist and are candidates.
        let ok: Vec<String> = hit
            .into_iter()
            .filter(|id| candidates.iter().any(|c| &c.id == id))
            .collect();
        return Selection {
            ids: with(ok),
            source: Source::Cache,
        };
    }
    let defaults = || {
        with(
            candidates
                .iter()
                .filter(|c| ts.skills.get(&c.id).is_some_and(|s| s.default))
                .map(|c| c.id.clone())
                .collect(),
        )
    };
    let Some(router) = router else {
        return Selection {
            ids: defaults(),
            source: Source::Default,
        };
    };
    if breaker.open().await {
        return Selection {
            ids: defaults(),
            source: Source::Breaker,
        };
    }
    breaker.record_router(ROUTER_CALL_NANOS).await;
    let answer = tokio::time::timeout(timeout, router.decide(&input, &candidates)).await;
    let Ok(Ok(probs)) = answer else {
        return Selection {
            ids: defaults(),
            source: Source::Default,
        };
    };
    let mut picked: Vec<(&Candidate, f64)> = candidates
        .iter()
        .filter_map(|c| {
            let p = *probs.get(&c.id)?;
            (p.is_finite() && p >= c.threshold).then_some((c, p))
        })
        .collect();
    // Most likely first, so the cap in `Library::assemble` drops the least likely.
    picked.sort_by(|a, b| b.1.total_cmp(&a.1).then_with(|| a.0.id.cmp(&b.0.id)));
    let mut ids: Vec<String> = picked.into_iter().map(|(c, _)| c.id.clone()).collect();
    // The per-skill questions are independent, so overlapping skills can all say yes. Per
    // exclusive group with more than one yes: one extra choice call, else the first listed.
    for group in &ts.exclusive {
        let hits: Vec<&Candidate> = group
            .iter()
            .filter(|g| ids.contains(g))
            .filter_map(|g| candidates.iter().find(|c| &c.id == g))
            .collect();
        if hits.len() < 2 {
            continue;
        }
        breaker.record_router(ROUTER_CALL_NANOS).await;
        let options: Vec<Candidate> = hits.iter().map(|c| (*c).clone()).collect();
        let won = match tokio::time::timeout(timeout, router.resolve(&input, &options)).await {
            Ok(Ok(p)) => options
                .iter()
                .filter_map(|c| Some((&c.id, *p.get(&c.id)?)))
                .filter(|(_, p)| p.is_finite())
                // Ties and ordering keep the earlier listed option.
                .fold(None::<(&String, f64)>, |b, x| match b {
                    Some(b) if b.1 >= x.1 => Some(b),
                    _ => Some(x),
                })
                .map(|(id, _)| id.clone()),
            _ => None,
        }
        .unwrap_or_else(|| hits[0].id.clone());
        ids.retain(|i| !group.contains(i) || *i == won);
    }
    cache.put(key, ids.clone()).await;
    Selection {
        ids: with(ids),
        source: Source::Model,
    }
}

static INSTALLED: std::sync::OnceLock<std::sync::Arc<dyn SkillRouter>> = std::sync::OnceLock::new();
static STATE: std::sync::OnceLock<(
    std::sync::Arc<dyn DecisionCache>,
    std::sync::Arc<dyn Breaker>,
)> = std::sync::OnceLock::new();
static LOCAL_CACHE: std::sync::OnceLock<MemoryCache> = std::sync::OnceLock::new();

/// Installs the process's decision model at startup (live mode only); without one every routed
/// prompt uses rules and the default set.
pub fn install(router: std::sync::Arc<dyn SkillRouter>) {
    let _ = INSTALLED.set(router);
}

pub fn installed() -> Option<&'static dyn SkillRouter> {
    INSTALLED.get().map(|r| r.as_ref())
}

/// Installs the shared decision cache and budget breaker (Redis in production). Without them the
/// process uses a local memory cache and no breaker.
pub fn install_state(
    cache: std::sync::Arc<dyn DecisionCache>,
    breaker: std::sync::Arc<dyn Breaker>,
) {
    let _ = STATE.set((cache, breaker));
}

/// The decision cache (disposable): shared when installed, else process-local.
pub fn shared_cache() -> &'static dyn DecisionCache {
    match STATE.get() {
        Some((c, _)) => c.as_ref(),
        None => LOCAL_CACHE.get_or_init(MemoryCache::default),
    }
}

pub fn shared_breaker() -> &'static dyn Breaker {
    match STATE.get() {
        Some((_, b)) => b.as_ref(),
        None => &NoBreaker,
    }
}

/// Main-model spend of one bounded run, for the breaker's day total. Never fails or blocks.
pub async fn note_engine_spend(nanos: u64) {
    if nanos > 0 {
        shared_breaker().record_engine(nanos).await;
    }
}

/// Estimated cost of one router call in nanodollars (spike: 0.0000089 USD, rounded up). The
/// Decisions API reports no cost to us, so the breaker counts this estimate per call.
pub const ROUTER_CALL_NANOS: u64 = 9_000;
/// The router may always spend this much per day before the ratio applies (0.001 USD).
pub const BREAKER_FLOOR_NANOS: u64 = 1_000_000;

/// Daily budget breaker: the router's spend may not exceed 1 percent of the day's main-model
/// spend (past a small floor). Open means "skip the model, use the default set".
pub fn breaker_open(router_nanos: u64, engine_nanos: u64) -> bool {
    router_nanos > BREAKER_FLOOR_NANOS && router_nanos.saturating_mul(100) > engine_nanos
}

#[async_trait]
pub trait Breaker: Send + Sync {
    async fn open(&self) -> bool;
    async fn record_router(&self, nanos: u64);
    async fn record_engine(&self, nanos: u64);
}

/// No breaker: the model is always allowed (tests, offline).
pub struct NoBreaker;

#[async_trait]
impl Breaker for NoBreaker {
    async fn open(&self) -> bool {
        false
    }
    async fn record_router(&self, _: u64) {}
    async fn record_engine(&self, _: u64) {}
}

/// Process-local counters with the real rule; Redis shares them across replicas.
#[derive(Default)]
pub struct MemoryBreaker {
    router: std::sync::atomic::AtomicU64,
    engine: std::sync::atomic::AtomicU64,
}

#[async_trait]
impl Breaker for MemoryBreaker {
    async fn open(&self) -> bool {
        use std::sync::atomic::Ordering::Relaxed;
        breaker_open(self.router.load(Relaxed), self.engine.load(Relaxed))
    }
    async fn record_router(&self, nanos: u64) {
        self.router
            .fetch_add(nanos, std::sync::atomic::Ordering::Relaxed);
    }
    async fn record_engine(&self, nanos: u64) {
        self.engine
            .fetch_add(nanos, std::sync::atomic::Ordering::Relaxed);
    }
}

#[cfg(test)]
mod tests;
