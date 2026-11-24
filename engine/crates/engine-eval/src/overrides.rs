//! Throwaway model overrides for comparison runs, without a branch per candidate model.
//!
//! `EVAL_MODEL_OVERRIDE="lesson=vendor/model@low>fallback-a>fallback-b,note=vendor/other"` replaces
//! the primary (and, when `>` follows, the fallback chain) of the named `models.toml` tasks for one
//! eval run; `@effort` sets `reasoning.effort` for that primary (otherwise none is sent, so name
//! the effort of the model you compare against). `>none` clears the fallbacks. `EVAL_GUARD_MODEL`
//! replaces the fast guard's decision model (see `engine-providers`). A run with an override is
//! stamped `status = "model_override"` and `prompt_revision` carries the override, so its report
//! can never satisfy the promotion gate.
use anyhow::{Context, Result, bail};
use engine_core::ProviderRouter;

#[derive(Debug, PartialEq)]
pub struct Override {
    pub task: String,
    pub model: String,
    pub effort: Option<String>,
    /// `None` keeps the configured chain.
    pub fallbacks: Option<Vec<String>>,
}

pub fn parse(spec: &str) -> Result<Vec<Override>> {
    spec.split(',')
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .map(|item| {
            let (task, rest) = item
                .split_once('=')
                .with_context(|| format!("override '{item}' is not task=model"))?;
            let mut chain = rest.split('>').map(str::trim);
            let head = chain.next().unwrap_or_default();
            let (model, effort) = match head.split_once('@') {
                Some((m, e)) => (m, Some(e.to_owned())),
                None => (head, None),
            };
            if task.trim().is_empty() || model.is_empty() {
                bail!("override '{item}' needs a task and a model");
            }
            let rest: Vec<String> = chain.map(str::to_owned).collect();
            let fallbacks = match rest.as_slice() {
                [] => None,
                [one] if one == "none" => Some(vec![]),
                _ => Some(rest),
            };
            Ok(Override {
                task: task.trim().to_owned(),
                model: model.to_owned(),
                effort,
                fallbacks,
            })
        })
        .collect()
}

/// Applies the env overrides to `router` and returns a label for the report (None when no
/// override is set).
pub fn apply_env(router: &mut ProviderRouter) -> Result<Option<String>> {
    let mut labels = Vec::new();
    if let Ok(spec) = std::env::var("EVAL_MODEL_OVERRIDE") {
        for o in parse(&spec)? {
            apply(router, &o)?;
            labels.push(format!("{}={}", o.task, o.model));
        }
    }
    if let Ok(m) = std::env::var("EVAL_GUARD_MODEL")
        && !m.trim().is_empty()
    {
        labels.push(format!("guard={}", m.trim()));
    }
    Ok((!labels.is_empty()).then(|| labels.join(",")))
}

pub fn apply(router: &mut ProviderRouter, o: &Override) -> Result<()> {
    let task = router
        .models
        .tasks
        .get_mut(&o.task)
        .with_context(|| format!("unknown task '{}' in EVAL_MODEL_OVERRIDE", o.task))?;
    task.reasoning.remove(&task.model);
    task.model = o.model.clone();
    task.reasoning_effort = o.effort.clone();
    if let Some(effort) = &o.effort {
        task.reasoning.insert(o.model.clone(), effort.clone());
    }
    if let Some(chain) = &o.fallbacks {
        task.fallback_models = chain.clone();
        task.fallback_model = chain.first().cloned().unwrap_or_default();
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_model_effort_and_chain() {
        let v = parse("lesson=a/b@low>c/d>e/f, note=g/h>none").unwrap();
        assert_eq!(v[0].model, "a/b");
        assert_eq!(v[0].effort.as_deref(), Some("low"));
        assert_eq!(v[0].fallbacks, Some(vec!["c/d".into(), "e/f".into()]));
        assert_eq!(v[1].fallbacks, Some(vec![]));
        assert!(parse("lesson").is_err());
        assert!(parse("=x").is_err());
    }

    #[test]
    fn replaces_the_route_and_rejects_unknown_tasks() {
        let provider = std::sync::Arc::new(engine_core::MockProvider);
        let mut router = ProviderRouter::new(provider.clone(), provider).unwrap();
        let o = &parse("lesson=a/b@low>none").unwrap()[0];
        apply(&mut router, o).unwrap();
        let r = router.models.tasks["lesson"].primary_route();
        assert_eq!(r.model, "a/b");
        assert!(r.fallbacks.is_empty());
        assert_eq!(r.reasoning.get("a/b").map(String::as_str), Some("low"));
        assert!(apply(&mut router, &parse("nope=a/b").unwrap()[0]).is_err());
    }
}
