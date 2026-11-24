use serde_json::Value;

// The webview and this module consume the same shortcut definitions.
const SHORTCUTS: &str = include_str!("../../src/features/hotkeys/shortcuts.json");

#[derive(Debug)]
pub(crate) struct MenuItemSpec {
    pub(crate) id: String,
    pub(crate) group: String,
    pub(crate) label: String,
    pub(crate) accelerator: Option<String>,
}

pub(crate) fn items() -> Vec<MenuItemSpec> {
    let shortcuts: Value = serde_json::from_str(SHORTCUTS).expect("valid shortcut registry");
    let mut items: Vec<_> = shortcuts
        .as_object()
        .expect("shortcut entries keyed by ID")
        .iter()
        .filter_map(|(id, entry)| {
            let menu = entry.get("menu")?;
            let text = |key| menu[key].as_str().expect("native menu text").to_owned();
            Some(MenuItemSpec {
                id: id.to_owned(),
                group: text("group"),
                label: text("label"),
                accelerator: menu["accelerator"].as_str().map(str::to_owned),
            })
        })
        .collect();
    items.sort_by(|a, b| a.accelerator.cmp(&b.accelerator));
    items
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn native_menu_accelerators_match_the_registry_bindings() {
        let manifest: Value = serde_json::from_str(SHORTCUTS).unwrap();
        let items = items();
        assert_eq!(items.len(), 7);
        assert!(items.iter().all(|item| !item.label.is_empty()));
        let accelerators: std::collections::HashSet<_> = items
            .iter()
            .filter_map(|item| item.accelerator.as_deref())
            .collect();
        assert_eq!(accelerators.len(), 5);
        let destinations: Vec<_> = items
            .iter()
            .filter(|item| item.group == "navigate")
            .map(|item| item.id.as_str())
            .collect();
        assert_eq!(
            destinations,
            ["go_learn", "go_courses", "go_friends", "go_notes"]
        );

        for item in items {
            let entry = &manifest[&item.id];
            match item.accelerator {
                Some(accelerator) => {
                    assert_eq!(entry["desktop"], true);
                    let binding = entry["bindings"][0].as_str().unwrap();
                    let base = binding.strip_prefix("Primary+").unwrap();
                    let key = if base == "," { "Comma" } else { base };
                    assert_eq!(accelerator, format!("CmdOrCtrl+{key}"));
                }
                None => assert!(matches!(
                    item.id.as_str(),
                    "show_help" | "toggle_full_screen"
                )),
            }
        }
    }
}
