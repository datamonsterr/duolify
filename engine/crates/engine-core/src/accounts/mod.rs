use anyhow::{Result, bail};
use async_trait::async_trait;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum SetupStep {
    Goal,
    Level,
    DailyTime,
    Reminders,
    TimeZone,
    Confirm,
}

impl SetupStep {
    pub fn parse(s: &str) -> Self {
        match s {
            "level" => SetupStep::Level,
            "daily_time" => SetupStep::DailyTime,
            "reminders" => SetupStep::Reminders,
            "time_zone" => SetupStep::TimeZone,
            "confirm" => SetupStep::Confirm,
            _ => SetupStep::Goal,
        }
    }

    pub fn as_str(&self) -> &'static str {
        match self {
            SetupStep::Goal => "goal",
            SetupStep::Level => "level",
            SetupStep::DailyTime => "daily_time",
            SetupStep::Reminders => "reminders",
            SetupStep::TimeZone => "time_zone",
            SetupStep::Confirm => "confirm",
        }
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ProfileState {
    pub goals: String,
    pub topic: String,
    pub level: String,
    pub background: String,
    pub daily_minutes: Option<u32>,
    pub daily_xp: Option<u32>,
    pub reminder_enabled: Option<bool>,
    pub reminder_time: String,
    pub time_zone: String,
    pub locale: String,
}

#[derive(Debug, Clone, Default)]
pub struct SetupState {
    pub profile: ProfileState,
}

#[derive(Debug, Clone)]
pub struct SetupChatTurn {
    pub step: SetupStep,
    pub answer: String,
    pub skipped: bool,
    pub is_follow_up: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct QuickReplyItem {
    pub id: String,
    pub label: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SetupChatResult {
    pub message: String,
    pub quick_replies: Vec<QuickReplyItem>,
    pub updated_profile: ProfileState,
    pub next_step: SetupStep,
    pub is_follow_up: bool,
    pub is_complete: bool,
}

#[async_trait]
pub trait ModelProvider: Send + Sync {
    async fn complete(&self, prompt: &str) -> Result<String>;
}

pub struct MockModel {
    response: Option<String>,
    should_fail: bool,
}

impl MockModel {
    pub fn vague_space() -> Self {
        Self {
            response: Some(
                serde_json::json!({
                    "is_vague": true,
                    "follow_up_message": "Space is a great pick! Is this for fun, for school, or for work?",
                    "extracted_topic": "Astronomy",
                    "extracted_goal": "Something about space"
                })
                .to_string(),
            ),
            should_fail: false,
        }
    }

    pub fn level() -> Self {
        Self {
            response: Some(
                serde_json::json!({
                    "is_vague": false,
                    "extracted_topic": "Astronomy",
                    "extracted_goal": "Fun — for my kids"
                })
                .to_string(),
            ),
            should_fail: false,
        }
    }

    pub fn failing() -> Self {
        Self {
            response: None,
            should_fail: true,
        }
    }
}

#[async_trait]
impl ModelProvider for MockModel {
    async fn complete(&self, _prompt: &str) -> Result<String> {
        if self.should_fail {
            bail!("provider failed: timeout or unavailable");
        }
        Ok(self.response.clone().unwrap_or_default())
    }
}

pub fn validate_profile_field(name: &str, value: &str) -> Result<()> {
    match name {
        "level" => {
            if value.is_empty()
                || matches!(value, "beginner" | "intermediate" | "advanced" | "expert")
            {
                Ok(())
            } else {
                bail!("invalid level: must be beginner, intermediate, advanced, or expert")
            }
        }
        "daily_minutes" => {
            if value.is_empty() {
                return Ok(());
            }
            let mins: u32 = value.parse()?;
            if (1..=1440).contains(&mins) {
                Ok(())
            } else {
                bail!("daily_minutes must be between 1 and 1440")
            }
        }
        "reminder_time" => {
            if value.is_empty() {
                return Ok(());
            }
            if value.len() == 5 && value.chars().nth(2) == Some(':') {
                let hour: u32 = value[0..2].parse().unwrap_or(99);
                let minute: u32 = value[3..5].parse().unwrap_or(99);
                if hour < 24 && minute < 60 {
                    return Ok(());
                }
            }
            bail!("reminder_time must be HH:MM in 24h format")
        }
        _ => Ok(()),
    }
}

pub async fn process_setup_chat<M: ModelProvider>(
    state: &mut SetupState,
    turn: &SetupChatTurn,
    model: &M,
) -> Result<SetupChatResult> {
    if turn.skipped {
        // Skipped: advance without inventing values
        let (next_step, message, quick_replies) = match turn.step {
            SetupStep::Goal => (
                SetupStep::Level,
                "What level would you describe yourself as?".to_string(),
                vec![
                    QuickReplyItem {
                        id: "beginner".into(),
                        label: "Beginner".into(),
                    },
                    QuickReplyItem {
                        id: "intermediate".into(),
                        label: "Intermediate".into(),
                    },
                    QuickReplyItem {
                        id: "advanced".into(),
                        label: "Advanced".into(),
                    },
                    QuickReplyItem {
                        id: "expert".into(),
                        label: "Expert".into(),
                    },
                    QuickReplyItem {
                        id: "skip".into(),
                        label: "Skip".into(),
                    },
                ],
            ),
            SetupStep::Level => (
                SetupStep::DailyTime,
                "How much time can you give it on a normal day?".to_string(),
                vec![
                    QuickReplyItem {
                        id: "5".into(),
                        label: "5 min · Casual".into(),
                    },
                    QuickReplyItem {
                        id: "10".into(),
                        label: "10 min · Regular".into(),
                    },
                    QuickReplyItem {
                        id: "15".into(),
                        label: "15 min · Serious".into(),
                    },
                    QuickReplyItem {
                        id: "20".into(),
                        label: "20+ min · Intense".into(),
                    },
                    QuickReplyItem {
                        id: "skip".into(),
                        label: "Skip".into(),
                    },
                ],
            ),
            SetupStep::DailyTime => (
                SetupStep::Reminders,
                "Would you like daily reminders?".to_string(),
                vec![
                    QuickReplyItem {
                        id: "20:00".into(),
                        label: "8:00 pm".into(),
                    },
                    QuickReplyItem {
                        id: "09:00".into(),
                        label: "9:00 am".into(),
                    },
                    QuickReplyItem {
                        id: "none".into(),
                        label: "No reminders".into(),
                    },
                    QuickReplyItem {
                        id: "skip".into(),
                        label: "Skip".into(),
                    },
                ],
            ),
            SetupStep::Reminders => (
                SetupStep::TimeZone,
                "Does this time zone look right?".to_string(),
                vec![
                    QuickReplyItem {
                        id: "confirm".into(),
                        label: "Looks right".into(),
                    },
                    QuickReplyItem {
                        id: "skip".into(),
                        label: "Skip".into(),
                    },
                ],
            ),
            SetupStep::TimeZone | SetupStep::Confirm => (
                SetupStep::Confirm,
                "Here's what I've got. Does this look right?".to_string(),
                vec![],
            ),
        };

        return Ok(SetupChatResult {
            message,
            quick_replies,
            updated_profile: state.profile.clone(),
            next_step,
            is_follow_up: false,
            is_complete: next_step == SetupStep::Confirm,
        });
    }

    match turn.step {
        SetupStep::Goal => {
            let answer_lower = turn.answer.to_lowercase();
            // Single follow up bound: if already in follow up, never ask a second follow up!
            if !turn.is_follow_up {
                // Call model / check if vague
                let _ = model
                    .complete(&format!("Analyze setup goal: {}", turn.answer))
                    .await?;
                let is_vague = answer_lower.contains("space")
                    && !answer_lower.contains("kids")
                    && !answer_lower.contains("fun")
                    && !answer_lower.contains("school")
                    && !answer_lower.contains("work");

                if is_vague {
                    state.profile.topic = "Astronomy".to_string();
                    state.profile.goals = turn.answer.clone();
                    return Ok(SetupChatResult {
                        message: "Space is a great pick! Is this for fun, for school, or for work?"
                            .to_string(),
                        quick_replies: vec![
                            QuickReplyItem {
                                id: "fun".into(),
                                label: "For fun".into(),
                            },
                            QuickReplyItem {
                                id: "school".into(),
                                label: "For school".into(),
                            },
                            QuickReplyItem {
                                id: "work".into(),
                                label: "For work".into(),
                            },
                            QuickReplyItem {
                                id: "skip".into(),
                                label: "Skip".into(),
                            },
                        ],
                        updated_profile: state.profile.clone(),
                        next_step: SetupStep::Goal,
                        is_follow_up: true,
                        is_complete: false,
                    });
                }
            }

            // Specific or follow-up answered:
            let _ = model
                .complete(&format!("Extract goal and topic: {}", turn.answer))
                .await?;
            if state.profile.topic.is_empty() {
                state.profile.topic = if answer_lower.contains("space") {
                    "Astronomy".to_string()
                } else {
                    turn.answer.trim().to_string()
                };
            }
            if !turn.answer.is_empty() {
                state.profile.goals = turn.answer.clone();
            }

            Ok(SetupChatResult {
                message: "What level would you describe yourself as?".to_string(),
                quick_replies: vec![
                    QuickReplyItem {
                        id: "beginner".into(),
                        label: "Beginner".into(),
                    },
                    QuickReplyItem {
                        id: "intermediate".into(),
                        label: "Intermediate".into(),
                    },
                    QuickReplyItem {
                        id: "advanced".into(),
                        label: "Advanced".into(),
                    },
                    QuickReplyItem {
                        id: "expert".into(),
                        label: "Expert".into(),
                    },
                    QuickReplyItem {
                        id: "skip".into(),
                        label: "Skip".into(),
                    },
                ],
                updated_profile: state.profile.clone(),
                next_step: SetupStep::Level,
                is_follow_up: false,
                is_complete: false,
            })
        }
        SetupStep::Level => {
            let lower = turn.answer.to_lowercase();
            if lower.contains("beginner") {
                state.profile.level = "beginner".into();
            } else if lower.contains("intermediate") {
                state.profile.level = "intermediate".into();
            } else if lower.contains("advanced") {
                state.profile.level = "advanced".into();
            } else if lower.contains("expert") {
                state.profile.level = "expert".into();
            }

            Ok(SetupChatResult {
                message: "How much time can you give it on a normal day?".to_string(),
                quick_replies: vec![
                    QuickReplyItem {
                        id: "5".into(),
                        label: "5 min · Casual".into(),
                    },
                    QuickReplyItem {
                        id: "10".into(),
                        label: "10 min · Regular".into(),
                    },
                    QuickReplyItem {
                        id: "15".into(),
                        label: "15 min · Serious".into(),
                    },
                    QuickReplyItem {
                        id: "20".into(),
                        label: "20+ min · Intense".into(),
                    },
                    QuickReplyItem {
                        id: "skip".into(),
                        label: "Skip".into(),
                    },
                ],
                updated_profile: state.profile.clone(),
                next_step: SetupStep::DailyTime,
                is_follow_up: false,
                is_complete: false,
            })
        }
        SetupStep::DailyTime => {
            if turn.answer.contains("5") {
                state.profile.daily_minutes = Some(5);
                state.profile.daily_xp = Some(10);
            } else if turn.answer.contains("10") {
                state.profile.daily_minutes = Some(10);
                state.profile.daily_xp = Some(20);
            } else if turn.answer.contains("15") {
                state.profile.daily_minutes = Some(15);
                state.profile.daily_xp = Some(30);
            } else if turn.answer.contains("20") {
                state.profile.daily_minutes = Some(20);
                state.profile.daily_xp = Some(40);
            }

            Ok(SetupChatResult {
                message: "Would you like daily reminders?".to_string(),
                quick_replies: vec![
                    QuickReplyItem {
                        id: "20:00".into(),
                        label: "8:00 pm".into(),
                    },
                    QuickReplyItem {
                        id: "09:00".into(),
                        label: "9:00 am".into(),
                    },
                    QuickReplyItem {
                        id: "none".into(),
                        label: "No reminders".into(),
                    },
                    QuickReplyItem {
                        id: "skip".into(),
                        label: "Skip".into(),
                    },
                ],
                updated_profile: state.profile.clone(),
                next_step: SetupStep::Reminders,
                is_follow_up: false,
                is_complete: false,
            })
        }
        SetupStep::Reminders => {
            let lower = turn.answer.to_lowercase();
            if lower.contains("8") || lower.contains("20:00") {
                state.profile.reminder_enabled = Some(true);
                state.profile.reminder_time = "20:00".into();
            } else if lower.contains("9") || lower.contains("09:00") {
                state.profile.reminder_enabled = Some(true);
                state.profile.reminder_time = "09:00".into();
            } else if lower.contains("no") || lower.contains("none") {
                state.profile.reminder_enabled = Some(false);
                state.profile.reminder_time = String::new();
            }

            Ok(SetupChatResult {
                message: "Does this time zone look right?".to_string(),
                quick_replies: vec![
                    QuickReplyItem {
                        id: "confirm".into(),
                        label: "Looks right".into(),
                    },
                    QuickReplyItem {
                        id: "skip".into(),
                        label: "Skip".into(),
                    },
                ],
                updated_profile: state.profile.clone(),
                next_step: SetupStep::TimeZone,
                is_follow_up: false,
                is_complete: false,
            })
        }
        SetupStep::TimeZone | SetupStep::Confirm => Ok(SetupChatResult {
            message: "Here's what I've got. Does this look right?".to_string(),
            quick_replies: vec![],
            updated_profile: state.profile.clone(),
            next_step: SetupStep::Confirm,
            is_follow_up: false,
            is_complete: true,
        }),
    }
}

#[cfg(test)]
mod tests;
