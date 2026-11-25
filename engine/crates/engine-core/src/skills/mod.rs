//! Prompt skills (ADR 0025): a task's always-on core plus optional skill modules loaded from
//! `engine/config/skills/<task>.toml`. The files are embedded and fingerprinted by
//! `promotion::SOURCES`, so any edit makes recorded live evidence stale. A task stays
//! `monolithic` (today's single prompt, byte for byte) until its rollout PR flips `prompt_mode`.

use serde::Deserialize;
use std::collections::{BTreeMap, BTreeSet};
use std::fmt;

/// Embedded skill files, listed here and in `promotion::SOURCES`.
pub const BUNDLED: &[(&str, &str)] = &[
    (
        "engine/config/skills/lesson_slides.toml",
        include_str!("../../../../config/skills/lesson_slides.toml"),
    ),
    (
        "engine/config/skills/tutor_chat.toml",
        include_str!("../../../../config/skills/tutor_chat.toml"),
    ),
    (
        "engine/config/skills/course_preview.toml",
        include_str!("../../../../config/skills/course_preview.toml"),
    ),
    (
        "engine/config/skills/outline_replan.toml",
        include_str!("../../../../config/skills/outline_replan.toml"),
    ),
    (
        "engine/config/skills/course_outline.toml",
        include_str!("../../../../config/skills/course_outline.toml"),
    ),
    (
        "engine/config/skills/outline_chat_change.toml",
        include_str!("../../../../config/skills/outline_chat_change.toml"),
    ),
];

/// A skill body is at most this many estimated tokens.
pub const MAX_BODY_TOKENS: u32 = 150;
/// Allowed difference between a declared and an estimated token count.
const DRIFT: f64 = 0.10;
/// Tasks a skill file may name: the launch tasks and the consolidated input guard.
fn known_task(t: &str) -> bool {
    t == "input_guard" || crate::promotion::LAUNCH_TASKS.contains(&t)
}

/// Estimated tokens of `text` (4 characters each, rounded up): cheap, deterministic, and close
/// enough for budgets; the provider's own count decides billing.
pub fn estimate_tokens(text: &str) -> u32 {
    (text.chars().count() as u32).div_ceil(4)
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize, Default)]
#[serde(rename_all = "snake_case")]
pub enum PromptMode {
    #[default]
    Monolithic,
    Routed,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SkillError {
    Parse {
        file: String,
        message: String,
    },
    UnknownTask(String),
    DuplicateId {
        task: String,
        id: String,
    },
    Invalid {
        file: String,
        message: String,
    },
    ConflictingDefaults {
        task: String,
        a: String,
        b: String,
    },
    OverBudget {
        task: String,
        tokens: u32,
        budget: u32,
    },
    TokenDrift {
        id: String,
        declared: u32,
        estimated: u32,
    },
    UnknownSkill {
        task: String,
        id: String,
    },
}

impl fmt::Display for SkillError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{self:?}")
    }
}
impl std::error::Error for SkillError {}

#[derive(Debug, Clone, Deserialize)]
struct RawFile {
    task: String,
    #[serde(default)]
    prompt_mode: PromptMode,
    routing: Routing,
    #[serde(default)]
    exclusive: Vec<Vec<String>>,
    #[serde(default, rename = "skill")]
    skills: Vec<RawSkill>,
}

/// Per-task limits of the assembled skill set.
#[derive(Debug, Clone, Copy, Deserialize, PartialEq, Eq)]
pub struct Routing {
    pub max_skills: usize,
    pub budget_tokens: u32,
}

#[derive(Debug, Clone, Deserialize)]
struct RawSkill {
    id: Option<String>,
    description: Option<String>,
    tasks: Option<Vec<String>>,
    version: Option<u32>,
    order: Option<u32>,
    tokens: Option<u32>,
    #[serde(default)]
    default: bool,
    #[serde(default)]
    conflicts: Vec<String>,
    #[serde(default)]
    reasoning_effort_min: String,
    body: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Skill {
    pub id: String,
    pub description: String,
    pub version: u32,
    pub order: u32,
    pub tokens: u32,
    pub default: bool,
    pub conflicts: Vec<String>,
    pub reasoning_effort_min: String,
    pub body: String,
}

#[derive(Debug, Clone)]
pub struct TaskSkills {
    pub mode: PromptMode,
    pub routing: Routing,
    /// Groups of overlapping model-routed skills of which at most one is loaded; the first
    /// listed wins when the router cannot choose (ADR 0025 D20).
    pub exclusive: Vec<Vec<String>>,
    pub skills: BTreeMap<String, Skill>,
}

/// The assembled prompt: `system` is the byte-stable prefix (core, then skills), `user` the data.
/// A monolithic prompt keeps today's single user message and an empty `system`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Prompt {
    pub system: String,
    pub user: String,
}

#[derive(Debug, Clone, Default)]
pub struct Library {
    tasks: BTreeMap<String, TaskSkills>,
}

fn snake(id: &str) -> bool {
    !id.is_empty()
        && id.starts_with(|c: char| c.is_ascii_lowercase())
        && id
            .chars()
            .all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '_')
}

/// The embedded library, checked once; a bad bundled file stops the process at first use.
pub fn bundled() -> &'static Library {
    static LIB: std::sync::OnceLock<Library> = std::sync::OnceLock::new();
    LIB.get_or_init(|| Library::bundled().expect("engine/config/skills is valid"))
}

impl Library {
    /// The embedded library; a boot check (`expect` at startup) rather than a runtime surprise.
    pub fn bundled() -> Result<Library, SkillError> {
        Self::from_sources(BUNDLED)
    }

    pub fn from_sources(sources: &[(&str, &str)]) -> Result<Library, SkillError> {
        let mut tasks = BTreeMap::new();
        for (file, text) in sources {
            let raw: RawFile = toml::from_str(text).map_err(|e| SkillError::Parse {
                file: (*file).into(),
                message: e.to_string(),
            })?;
            if !known_task(&raw.task) {
                return Err(SkillError::UnknownTask(raw.task));
            }
            let invalid = |message: String| SkillError::Invalid {
                file: (*file).into(),
                message,
            };
            let mut skills = BTreeMap::new();
            for s in raw.skills {
                let id =
                    s.id.clone()
                        .ok_or_else(|| invalid("skill without id".into()))?;
                if !snake(&id) {
                    return Err(invalid(format!(
                        "skill id {id:?} is not lowercase snake case"
                    )));
                }
                let field = |name: &str| invalid(format!("skill {id}: missing {name}"));
                let (
                    Some(description),
                    Some(listed),
                    Some(version),
                    Some(order),
                    Some(tokens),
                    Some(body),
                ) = (s.description, s.tasks, s.version, s.order, s.tokens, s.body)
                else {
                    return Err(field("description, tasks, version, order, tokens or body"));
                };
                if !listed.contains(&raw.task) {
                    return Err(invalid(format!(
                        "skill {id} does not list task {}",
                        raw.task
                    )));
                }
                let estimated = estimate_tokens(&body);
                if estimated > MAX_BODY_TOKENS {
                    return Err(invalid(format!(
                        "skill {id} body is {estimated} tokens, over {MAX_BODY_TOKENS}"
                    )));
                }
                if (f64::from(tokens) - f64::from(estimated)).abs()
                    > DRIFT * f64::from(estimated).max(10.0)
                {
                    return Err(SkillError::TokenDrift {
                        id,
                        declared: tokens,
                        estimated,
                    });
                }
                let skill = Skill {
                    id: id.clone(),
                    description,
                    version,
                    order,
                    tokens,
                    default: s.default,
                    conflicts: s.conflicts,
                    reasoning_effort_min: s.reasoning_effort_min,
                    body,
                };
                if skills.insert(id.clone(), skill).is_some() {
                    return Err(SkillError::DuplicateId { task: raw.task, id });
                }
            }
            let ts = TaskSkills {
                mode: raw.prompt_mode,
                routing: raw.routing,
                exclusive: raw.exclusive,
                skills,
            };
            ts.check(&raw.task)?;
            if tasks.insert(raw.task.clone(), ts).is_some() {
                return Err(invalid(format!("task {} is defined twice", raw.task)));
            }
        }
        Ok(Library { tasks })
    }

    /// Prompt mode of `task`; tasks without a file are monolithic.
    pub fn mode(&self, task: &str) -> PromptMode {
        self.tasks
            .get(task)
            .map_or(PromptMode::Monolithic, |t| t.mode)
    }

    pub fn task(&self, task: &str) -> Option<&TaskSkills> {
        self.tasks.get(task)
    }

    /// Builds the prompt. Monolithic returns `core` as the single user message (the legacy
    /// prompt, unchanged). Routed picks from `ids` in priority order (duplicates and conflicts
    /// with an earlier pick dropped, then the count cap and token budget), orders the picks by
    /// `(order, id)` and puts them after `core` in `system`; `data` is the user message.
    pub fn assemble(
        &self,
        task: &str,
        core: &str,
        ids: &[String],
        data: &str,
    ) -> Result<Prompt, SkillError> {
        let Some(ts) = self
            .tasks
            .get(task)
            .filter(|t| t.mode == PromptMode::Routed)
        else {
            return Ok(Prompt {
                system: String::new(),
                user: core.to_string(),
            });
        };
        let mut picked: Vec<&Skill> = Vec::new();
        let mut spent = 0u32;
        for id in ids {
            let s = ts.skills.get(id).ok_or_else(|| SkillError::UnknownSkill {
                task: task.into(),
                id: id.clone(),
            })?;
            let clash = picked.iter().any(|p| {
                p.id == s.id || p.conflicts.contains(&s.id) || s.conflicts.contains(&p.id)
            });
            if clash
                || picked.len() >= ts.routing.max_skills
                || spent + s.tokens > ts.routing.budget_tokens
            {
                continue;
            }
            spent += s.tokens;
            picked.push(s);
        }
        picked.sort_by(|a, b| (a.order, &a.id).cmp(&(b.order, &b.id)));
        let mut system = core.to_string();
        for s in picked {
            system.push_str("\n\n");
            system.push_str(&s.body);
        }
        Ok(Prompt {
            system,
            user: data.to_string(),
        })
    }
}

impl TaskSkills {
    fn check(&self, task: &str) -> Result<(), SkillError> {
        let defaults: Vec<&Skill> = self.skills.values().filter(|s| s.default).collect();
        for (i, a) in defaults.iter().enumerate() {
            for b in &defaults[i + 1..] {
                if a.conflicts.contains(&b.id) || b.conflicts.contains(&a.id) {
                    return Err(SkillError::ConflictingDefaults {
                        task: task.into(),
                        a: a.id.clone(),
                        b: b.id.clone(),
                    });
                }
            }
        }
        let tokens: u32 = defaults.iter().map(|s| s.tokens).sum();
        if tokens > self.routing.budget_tokens || defaults.len() > self.routing.max_skills {
            return Err(SkillError::OverBudget {
                task: task.into(),
                tokens,
                budget: self.routing.budget_tokens,
            });
        }
        let ids: BTreeSet<&String> = self.skills.keys().collect();
        for g in &self.exclusive {
            if g.len() < 2 {
                return Err(SkillError::Invalid {
                    file: task.into(),
                    message: "exclusive group under 2 skills".into(),
                });
            }
            if let Some(u) = g.iter().find(|i| !ids.contains(i)) {
                return Err(SkillError::UnknownSkill {
                    task: task.into(),
                    id: u.clone(),
                });
            }
        }
        for s in self.skills.values() {
            if let Some(c) = s.conflicts.iter().find(|c| !ids.contains(c)) {
                return Err(SkillError::UnknownSkill {
                    task: task.into(),
                    id: c.clone(),
                });
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests;
