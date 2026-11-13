use std::collections::HashSet;

pub const CANONICAL_TASKS: [&str; 6] = [
    "course_preview",
    "course_outline",
    "lesson_slides",
    "lesson_test",
    "tutor_chat",
    "note_enrich",
];

pub fn canonical_task_name(task: &str) -> String {
    match task {
        "preview" | "course_preview" => "course_preview".to_string(),
        "outline" | "course_outline" => "course_outline".to_string(),
        "lesson" | "slides" | "lesson_slides" => "lesson_slides".to_string(),
        "test" | "lesson_test" => "lesson_test".to_string(),
        "chat" | "tutor_chat" => "tutor_chat".to_string(),
        "note" | "note_enrich" => "note_enrich".to_string(),
        "narration" | "lesson_narration" => "lesson_narration".to_string(),
        "lesson_test_variant" | "variants" => "lesson_test_variant".to_string(),
        other => other.to_string(),
    }
}

pub fn model_route_for_task(task: &str) -> String {
    let canonical = canonical_task_name(task);
    match canonical.as_str() {
        "course_preview" => "preview".to_string(),
        "course_outline" => "outline".to_string(),
        "lesson_slides" => "lesson".to_string(),
        "lesson_test" => "lesson".to_string(),
        "tutor_chat" => "chat".to_string(),
        "note_enrich" => "note".to_string(),
        "course_brief_chat" => "course_brief_chat".to_string(),
        "setup_chat" => "setup_chat".to_string(),
        "outline_chat_change" => "outline_chat_change".to_string(),
        "credit_envelope" => "preview".to_string(),
        "variants" | "lesson_test_variant" => "lesson".to_string(),
        "narration" | "lesson_narration" => "lesson".to_string(),
        "screening" => "chat".to_string(),
        "steering" => "outline".to_string(),
        other => other.to_string(),
    }
}

#[derive(Debug, Clone)]
pub struct TaskRegistry {
    canonical_tasks: HashSet<String>,
    registered_extensions: HashSet<String>,
}

impl Default for TaskRegistry {
    fn default() -> Self {
        let mut canonical_tasks = HashSet::new();
        for &task in &CANONICAL_TASKS {
            canonical_tasks.insert(task.to_string());
        }
        let mut registered_extensions = HashSet::new();
        registered_extensions.insert("setup_chat".to_string());
        registered_extensions.insert("course_brief_chat".to_string());
        registered_extensions.insert("outline_chat_change".to_string());
        registered_extensions.insert("credit_envelope".to_string());
        registered_extensions.insert("lesson_narration".to_string());
        registered_extensions.insert("lesson_test_variant".to_string());
        registered_extensions.insert("course_screen".to_string());
        for task in [
            "character_spec",
            "character_screen",
            "character_injection",
            "character_persona_lesson",
            "character_reactions",
        ] {
            registered_extensions.insert(task.into());
        }

        Self {
            canonical_tasks,
            registered_extensions,
        }
    }
}

impl TaskRegistry {
    pub fn register_extension(&mut self, name: &str) {
        self.registered_extensions.insert(name.to_string());
    }

    pub fn is_valid_task(&self, task: &str) -> bool {
        let canonical = canonical_task_name(task);
        self.canonical_tasks.contains(&canonical)
            || self.registered_extensions.contains(task)
            || self.registered_extensions.contains(&canonical)
    }

    pub fn missing_required_tasks(&self, present_tasks: &[String]) -> Vec<String> {
        let present_canonical: HashSet<String> = present_tasks
            .iter()
            .map(|t| canonical_task_name(t))
            .collect();
        let mut missing: Vec<String> = self
            .canonical_tasks
            .iter()
            .filter(|t| !present_canonical.contains(*t))
            .cloned()
            .collect();
        missing.sort();
        missing
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_canonical_mapping() {
        assert_eq!(canonical_task_name("preview"), "course_preview");
        assert_eq!(canonical_task_name("outline"), "course_outline");
        assert_eq!(canonical_task_name("lesson"), "lesson_slides");
        assert_eq!(canonical_task_name("slides"), "lesson_slides");
        assert_eq!(canonical_task_name("test"), "lesson_test");
        assert_eq!(canonical_task_name("chat"), "tutor_chat");
        assert_eq!(canonical_task_name("note"), "note_enrich");
        assert_eq!(canonical_task_name("narration"), "lesson_narration");
        assert_eq!(canonical_task_name("custom"), "custom");
    }

    #[test]
    fn test_model_route_mapping() {
        assert_eq!(model_route_for_task("course_preview"), "preview");
        assert_eq!(model_route_for_task("preview"), "preview");
        assert_eq!(model_route_for_task("lesson_slides"), "lesson");
        assert_eq!(model_route_for_task("lesson_test"), "lesson");
        assert_eq!(model_route_for_task("variants"), "lesson");
    }

    #[test]
    fn test_task_registry_extensions_and_feature_gate() {
        let mut reg = TaskRegistry::default();
        reg.register_extension("variants");
        reg.register_extension("narration");
        reg.register_extension("screening");
        reg.register_extension("steering");

        assert!(reg.is_valid_task("variants"));
        assert!(reg.is_valid_task("course_preview"));

        let present = vec![
            "course_preview".into(),
            "course_outline".into(),
            "lesson_slides".into(),
        ];
        let missing = reg.missing_required_tasks(&present);
        assert_eq!(missing, vec!["lesson_test", "note_enrich", "tutor_chat"]);
    }
}
