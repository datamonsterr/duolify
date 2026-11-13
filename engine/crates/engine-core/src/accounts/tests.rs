use super::*;

#[tokio::test]
async fn test_single_followup_bound() {
    let mut state = SetupState::default();

    // Turn 1: vague input
    let req = SetupChatTurn {
        step: SetupStep::Goal,
        answer: "something about space".to_string(),
        skipped: false,
        is_follow_up: false,
    };
    let res = process_setup_chat(&mut state, &req, &MockModel::vague_space())
        .await
        .unwrap();

    assert!(res.is_follow_up);
    assert_eq!(res.next_step, SetupStep::Goal);
    assert!(res.message.contains("fun, for school, or for work"));

    // Turn 2: answer to follow-up -> MUST advance to Level, NOT ask a 2nd follow-up
    let req2 = SetupChatTurn {
        step: SetupStep::Goal,
        answer: "Fun — for my kids".to_string(),
        skipped: false,
        is_follow_up: true,
    };
    let res2 = process_setup_chat(&mut state, &req2, &MockModel::level())
        .await
        .unwrap();

    assert!(!res2.is_follow_up);
    assert_eq!(res2.next_step, SetupStep::Level);
}

#[tokio::test]
async fn test_skipped_fields_do_not_invent_data() {
    let mut state = SetupState::default();

    let req = SetupChatTurn {
        step: SetupStep::Goal,
        answer: String::new(),
        skipped: true,
        is_follow_up: false,
    };
    let res = process_setup_chat(&mut state, &req, &MockModel::level())
        .await
        .unwrap();

    assert!(!res.is_follow_up);
    assert_eq!(res.next_step, SetupStep::Level);
    // Profile goal/topic must be empty, not invented
    assert_eq!(state.profile.goals, "");
    assert_eq!(state.profile.topic, "");
}

#[tokio::test]
async fn test_summary_validation() {
    // Valid level
    assert!(validate_profile_field("level", "beginner").is_ok());
    assert!(validate_profile_field("level", "invalid_level").is_err());

    // Valid daily minutes
    assert!(validate_profile_field("daily_minutes", "10").is_ok());
    assert!(validate_profile_field("daily_minutes", "5000").is_err());

    // Valid reminder time
    assert!(validate_profile_field("reminder_time", "20:00").is_ok());
    assert!(validate_profile_field("reminder_time", "99:99").is_err());
}

#[tokio::test]
async fn test_bounded_failures() {
    let mut state = SetupState::default();

    let req = SetupChatTurn {
        step: SetupStep::Goal,
        answer: "astronomy".to_string(),
        skipped: false,
        is_follow_up: false,
    };

    // Failing model returns error without corrupting state
    let err = process_setup_chat(&mut state, &req, &MockModel::failing())
        .await
        .unwrap_err();

    assert!(err.to_string().contains("provider failed"));
    assert_eq!(state.profile.topic, "");
}
