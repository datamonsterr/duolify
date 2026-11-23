use super::*;

fn lib(src: &str) -> Result<Library, SkillError> {
    Library::from_sources(&[("engine/config/skills/test.toml", src)])
}

const HEAD: &str = "task = \"lesson_slides\"\nprompt_mode = \"routed\"\n[routing]\nmax_skills = 2\nbudget_tokens = 100\n";

fn skill(id: &str, order: u32, extra: &str) -> String {
    format!(
        "[[skill]]\nid = \"{id}\"\ndescription = \"d\"\ntasks = [\"lesson_slides\"]\nversion = 1\norder = {order}\ntokens = 3\nbody = \"abcdefghijkl\"\n{extra}\n"
    )
}

#[test]
fn bundled_library_boots_and_every_task_stays_monolithic() {
    let l = Library::bundled().expect("bundled skills boot");
    for task in ["lesson_slides", "tutor_chat"] {
        assert_eq!(l.mode(task), PromptMode::Monolithic, "{task}");
    }
    assert_eq!(l.mode("course_outline"), PromptMode::Monolithic);
}

#[test]
fn monolithic_assembly_returns_the_legacy_prompt_unchanged() {
    let l = Library::bundled().unwrap();
    let p = l
        .assemble("lesson_slides", "LEGACY PROMPT", &["x".into()], "data")
        .unwrap();
    assert_eq!(
        p,
        Prompt {
            system: String::new(),
            user: "LEGACY PROMPT".into()
        }
    );
}

#[test]
fn boot_check_rejects_bad_files_with_typed_errors() {
    assert!(matches!(
        lib("task = \"nope\"\nprompt_mode = \"routed\"\n[routing]\nmax_skills = 1\nbudget_tokens = 1\n"),
        Err(SkillError::UnknownTask(t)) if t == "nope"
    ));
    assert!(matches!(
        lib(&format!("{HEAD}{}{}", skill("a", 1, ""), skill("a", 2, ""))),
        Err(SkillError::DuplicateId { id, .. }) if id == "a"
    ));
    assert!(matches!(
        lib(&format!(
            "{HEAD}[[skill]]\nid = \"a\"\ntasks = [\"lesson_slides\"]\n"
        )),
        Err(SkillError::Invalid { .. })
    ));
    assert!(matches!(
        lib(&format!(
            "{HEAD}{}{}",
            skill("a", 1, "default = true\nconflicts = [\"b\"]"),
            skill("b", 2, "default = true")
        )),
        Err(SkillError::ConflictingDefaults { .. })
    ));
    let big = "x".repeat(404);
    assert!(matches!(
        lib(&format!(
            "{HEAD}[[skill]]\nid = \"a\"\ndescription = \"d\"\ntasks = [\"lesson_slides\"]\nversion = 1\norder = 1\ntokens = 101\ndefault = true\nbody = \"{big}\"\n"
        )),
        Err(SkillError::OverBudget { .. })
    ));
    assert!(matches!(
        lib(&format!("{HEAD}{}", skill("Bad-Id", 1, ""))),
        Err(SkillError::Invalid { .. })
    ));
}

#[test]
fn token_counts_must_match_the_estimate_within_ten_percent() {
    // 100 chars = 25 estimated tokens.
    let body = "x".repeat(100);
    let ok = format!(
        "{HEAD}{}",
        skill("a", 1, &format!("tokens = 26\nbody = \"{body}\""))
            .replace("tokens = 3\n", "")
            .replace("body = \"abcdefghijkl\"\n", "")
    );
    assert!(lib(&ok).is_ok(), "{:?}", lib(&ok).err());
    let bad = ok.replace("tokens = 26", "tokens = 40");
    assert!(matches!(lib(&bad), Err(SkillError::TokenDrift { .. })));
}

#[test]
fn assembly_is_deterministic_deduped_capped_and_budgeted() {
    let src = format!(
        "{HEAD}{}{}{}",
        skill("late", 30, ""),
        skill("early", 10, ""),
        skill("mid", 20, "")
    );
    let l = lib(&src).unwrap();
    let ids = |v: &[&str]| v.iter().map(|s| s.to_string()).collect::<Vec<_>>();
    // Priority order picks; assembly order is by (order, id); duplicates collapse.
    let p = l
        .assemble(
            "lesson_slides",
            "CORE",
            &ids(&["late", "early", "late", "mid"]),
            "DATA",
        )
        .unwrap();
    assert_eq!(p.system, "CORE\n\nabcdefghijkl\n\nabcdefghijkl");
    assert_eq!(p.user, "DATA");
    // Cap of 2: "late" and "early" win (first two distinct), so the selection is stable.
    let again = l
        .assemble(
            "lesson_slides",
            "CORE",
            &ids(&["late", "early", "mid"]),
            "DATA",
        )
        .unwrap();
    assert_eq!(p, again);
    assert!(matches!(
        l.assemble("lesson_slides", "CORE", &ids(&["ghost"]), "DATA"),
        Err(SkillError::UnknownSkill { .. })
    ));
}

#[test]
fn a_conflicting_pair_is_never_assembled_together() {
    let src = format!(
        "{HEAD}{}{}",
        skill("a", 1, "conflicts = [\"b\"]"),
        skill("b", 2, "")
    );
    let l = lib(&src).unwrap();
    let p = l
        .assemble("lesson_slides", "C", &["a".into(), "b".into()], "D")
        .unwrap();
    assert_eq!(
        p.system.matches("abcdefghijkl").count(),
        1,
        "first in priority wins"
    );
}

#[test]
fn skill_files_are_fingerprinted() {
    for (path, _) in BUNDLED {
        assert!(
            crate::promotion::SOURCES.iter().any(|(p, _)| p == path),
            "{path} missing from promotion::SOURCES"
        );
    }
}
