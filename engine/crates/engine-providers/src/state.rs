//! Shared router state (ADR 0025, task 4.2): the decision cache and the daily budget breaker live
//! in Redis so replicas agree and the engine stays stateless. Everything here is disposable: a Redis
//! error is a cache miss, and an unreadable breaker is an open breaker (skip the optional model,
//! use the default skill set). Generation never waits on or fails because of this module.
use anyhow::Result;
use async_trait::async_trait;
use engine_core::router::{Breaker, DecisionCache, breaker_open};
use std::{sync::Arc, time::Duration};

/// Decision cache TTL (spec: 30 days).
pub const DECISION_TTL_SECS: u64 = 30 * 24 * 3600;
const COUNTER_TTL_SECS: u64 = 2 * 24 * 3600;
const OP_TIMEOUT: Duration = Duration::from_millis(100);

/// The few Redis commands the router state needs.
#[async_trait]
pub trait Kv: Send + Sync {
    async fn get(&self, key: &str) -> Result<Option<String>>;
    async fn set_ex(&self, key: &str, value: &str, ttl_secs: u64) -> Result<()>;
    /// INCRBY then EXPIRE; returns the new total.
    async fn incr_ex(&self, key: &str, by: u64, ttl_secs: u64) -> Result<u64>;
}

pub struct RedisKv {
    client: redis::Client,
    conn: tokio::sync::OnceCell<redis::aio::ConnectionManager>,
}

impl RedisKv {
    pub fn open(url: &str) -> Result<Self> {
        Ok(Self {
            client: redis::Client::open(url)?,
            conn: Default::default(),
        })
    }
    async fn conn(&self) -> Result<redis::aio::ConnectionManager> {
        Ok(self
            .conn
            .get_or_try_init(|| async {
                let config = redis::aio::ConnectionManagerConfig::new()
                    .set_connection_timeout(Some(OP_TIMEOUT))
                    .set_response_timeout(Some(OP_TIMEOUT));
                redis::aio::ConnectionManager::new_with_config(self.client.clone(), config).await
            })
            .await?
            .clone())
    }
}

#[async_trait]
impl Kv for RedisKv {
    async fn get(&self, key: &str) -> Result<Option<String>> {
        let mut c = self.conn().await?;
        Ok(redis::cmd("GET").arg(key).query_async(&mut c).await?)
    }
    async fn set_ex(&self, key: &str, value: &str, ttl_secs: u64) -> Result<()> {
        let mut c = self.conn().await?;
        redis::cmd("SET")
            .arg(key)
            .arg(value)
            .arg("EX")
            .arg(ttl_secs)
            .query_async::<()>(&mut c)
            .await?;
        Ok(())
    }
    async fn incr_ex(&self, key: &str, by: u64, ttl_secs: u64) -> Result<u64> {
        let mut c = self.conn().await?;
        let total: u64 = redis::cmd("INCRBY")
            .arg(key)
            .arg(by)
            .query_async(&mut c)
            .await?;
        redis::cmd("EXPIRE")
            .arg(key)
            .arg(ttl_secs)
            .query_async::<()>(&mut c)
            .await?;
        Ok(total)
    }
}

/// Decision cache and breaker over any `Kv`.
pub struct SharedState<K: Kv> {
    kv: K,
    /// Unix seconds, replaceable in tests.
    now: fn() -> u64,
}

impl<K: Kv> SharedState<K> {
    pub fn new(kv: K) -> Self {
        Self { kv, now: unix_now }
    }
    fn day(&self) -> u64 {
        (self.now)() / 86_400
    }
    fn router_key(&self) -> String {
        format!("duolify:router:spend:{}", self.day())
    }
    fn engine_key(&self) -> String {
        format!("duolify:engine:spend:{}", self.day())
    }
}

fn unix_now() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_or(0, |d| d.as_secs())
}

#[async_trait]
impl<K: Kv> DecisionCache for SharedState<K> {
    async fn get(&self, key: &str) -> Option<Vec<String>> {
        let raw = self
            .kv
            .get(&format!("duolify:router:decision:{key}"))
            .await
            .ok()??;
        serde_json::from_str(&raw).ok()
    }
    async fn put(&self, key: String, ids: Vec<String>) {
        if let Ok(raw) = serde_json::to_string(&ids) {
            let _ = self
                .kv
                .set_ex(
                    &format!("duolify:router:decision:{key}"),
                    &raw,
                    DECISION_TTL_SECS,
                )
                .await;
        }
    }
}

#[async_trait]
impl<K: Kv> Breaker for SharedState<K> {
    async fn open(&self) -> bool {
        let read = |k: String| async move {
            self.kv
                .get(&k)
                .await
                .map(|v| v.and_then(|s| s.parse().ok()).unwrap_or(0u64))
        };
        match (read(self.router_key()).await, read(self.engine_key()).await) {
            (Ok(router), Ok(engine)) => breaker_open(router, engine),
            // Cannot count, so do not spend: the optional model is skipped.
            _ => true,
        }
    }
    async fn record_router(&self, nanos: u64) {
        let _ = self
            .kv
            .incr_ex(&self.router_key(), nanos, COUNTER_TTL_SECS)
            .await;
    }
    async fn record_engine(&self, nanos: u64) {
        let _ = self
            .kv
            .incr_ex(&self.engine_key(), nanos, COUNTER_TTL_SECS)
            .await;
    }
}

/// Installs the Redis-backed cache and breaker when `REDIS_URL` is set; otherwise the process keeps
/// its local memory cache and no breaker.
pub fn install_from_env() {
    let Some(url) = std::env::var("REDIS_URL").ok().filter(|u| !u.is_empty()) else {
        return;
    };
    match RedisKv::open(&url) {
        Ok(kv) => {
            let state = Arc::new(SharedState::new(kv));
            engine_core::router::install_state(state.clone(), state);
            tracing::info!("router cache and breaker use redis");
        }
        Err(_) => tracing::warn!("REDIS_URL is not usable; router state stays process-local"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{collections::HashMap, sync::Mutex};

    #[derive(Default)]
    struct Fake(Mutex<HashMap<String, String>>, Mutex<Vec<(String, u64)>>);

    #[async_trait]
    impl Kv for Fake {
        async fn get(&self, key: &str) -> Result<Option<String>> {
            Ok(self.0.lock().unwrap().get(key).cloned())
        }
        async fn set_ex(&self, key: &str, value: &str, ttl: u64) -> Result<()> {
            self.1.lock().unwrap().push((key.into(), ttl));
            self.0.lock().unwrap().insert(key.into(), value.into());
            Ok(())
        }
        async fn incr_ex(&self, key: &str, by: u64, ttl: u64) -> Result<u64> {
            self.1.lock().unwrap().push((key.into(), ttl));
            let mut m = self.0.lock().unwrap();
            let v = m.get(key).and_then(|s| s.parse::<u64>().ok()).unwrap_or(0) + by;
            m.insert(key.into(), v.to_string());
            Ok(v)
        }
    }

    struct Down;
    #[async_trait]
    impl Kv for Down {
        async fn get(&self, _: &str) -> Result<Option<String>> {
            anyhow::bail!("down")
        }
        async fn set_ex(&self, _: &str, _: &str, _: u64) -> Result<()> {
            anyhow::bail!("down")
        }
        async fn incr_ex(&self, _: &str, _: u64, _: u64) -> Result<u64> {
            anyhow::bail!("down")
        }
    }

    #[tokio::test]
    async fn decisions_round_trip_with_a_thirty_day_ttl() {
        let s = SharedState::new(Fake::default());
        assert_eq!(s.get("k").await, None);
        s.put("k".into(), vec!["a".into(), "b".into()]).await;
        assert_eq!(s.get("k").await, Some(vec!["a".into(), "b".into()]));
        assert_eq!(
            s.kv.1.lock().unwrap()[0],
            ("duolify:router:decision:k".into(), 2_592_000)
        );
    }

    #[tokio::test]
    async fn breaker_counts_per_utc_day_and_opens_past_one_percent() {
        let mut s = SharedState::new(Fake::default());
        s.now = || 86_400 * 10 + 5;
        assert!(!s.open().await);
        s.record_router(1_500_000).await;
        assert!(
            s.open().await,
            "no engine spend yet and the floor is passed"
        );
        s.record_engine(200_000_000).await;
        assert!(!s.open().await);
        // The next UTC day starts from zero.
        s.now = || 86_400 * 11;
        assert!(!s.open().await);
        assert!(
            s.kv.0
                .lock()
                .unwrap()
                .contains_key("duolify:router:spend:10")
        );
    }

    #[tokio::test]
    async fn redis_down_is_a_cache_miss_and_an_open_breaker() {
        let s = SharedState::new(Down);
        assert_eq!(s.get("k").await, None);
        s.put("k".into(), vec![]).await;
        s.record_router(1).await;
        assert!(s.open().await);
    }
}
