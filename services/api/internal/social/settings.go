package social

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/jackc/pgx/v5"
)

// Settings are the learner's own privacy controls.
type Settings struct {
	Discoverable        bool   `json:"discoverable"`
	RequestPolicy       string `json:"request_policy"`
	ShowStreakToFriends bool   `json:"show_streak_to_friends"`
	CourseInvitesFrom   string `json:"course_invites_from"`
}

// SettingsPatch changes only the fields that are set.
type SettingsPatch struct {
	Discoverable        *bool   `json:"discoverable"`
	RequestPolicy       *string `json:"request_policy"`
	ShowStreakToFriends *bool   `json:"show_streak_to_friends"`
	CourseInvitesFrom   *string `json:"course_invites_from"`
}

// ErrInvalidSettings covers every malformed or empty settings body.
var ErrInvalidSettings = errors.New("invalid settings")

func boolValue(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// ParseSettingsPatch reads a strict, size-bounded body with at least one known field.
func ParseSettingsPatch(r io.Reader) (SettingsPatch, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 1025))
	if err != nil || len(raw) > 1024 {
		return SettingsPatch{}, ErrInvalidSettings
	}
	var p SettingsPatch
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&p) != nil || !errors.Is(d.Decode(&extra), io.EOF) {
		return SettingsPatch{}, ErrInvalidSettings
	}
	if p.Discoverable == nil && p.RequestPolicy == nil && p.ShowStreakToFriends == nil && p.CourseInvitesFrom == nil {
		return SettingsPatch{}, ErrInvalidSettings
	}
	if p.RequestPolicy != nil {
		switch *p.RequestPolicy {
		case "everyone", "friends_of_friends", "nobody":
		default:
			return SettingsPatch{}, ErrInvalidSettings
		}
	}
	if p.CourseInvitesFrom != nil {
		switch *p.CourseInvitesFrom {
		case "friends", "everyone", "nobody":
		default:
			return SettingsPatch{}, ErrInvalidSettings
		}
	}
	return p, nil
}

var defaultSettings = Settings{Discoverable: true, RequestPolicy: "everyone", ShowStreakToFriends: true, CourseInvitesFrom: "friends"}

// Settings returns stored settings or the defaults without creating a row.
func (s *Service) Settings(ctx context.Context, learnerID string) (Settings, error) {
	out := defaultSettings
	err := s.pool.QueryRow(ctx, `SELECT discoverable,request_policy,show_streak_to_friends,course_invites_from FROM learner_social_settings WHERE learner_id=$1::uuid`, learnerID).
		Scan(&out.Discoverable, &out.RequestPolicy, &out.ShowStreakToFriends, &out.CourseInvitesFrom)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, err
	}
	return out, nil
}

// UpdateSettings applies a patch atomically and emits one event per changed setting.
func (s *Service) UpdateSettings(ctx context.Context, learnerID string, p SettingsPatch) (Settings, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Settings{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO learner_social_settings(learner_id) VALUES($1::uuid) ON CONFLICT DO NOTHING`, learnerID); err != nil {
		return Settings{}, err
	}
	var cur Settings
	if err = tx.QueryRow(ctx, `SELECT discoverable,request_policy,show_streak_to_friends,course_invites_from FROM learner_social_settings WHERE learner_id=$1::uuid FOR UPDATE`, learnerID).
		Scan(&cur.Discoverable, &cur.RequestPolicy, &cur.ShowStreakToFriends, &cur.CourseInvitesFrom); err != nil {
		return Settings{}, err
	}
	next := cur
	changed := map[string]string{}
	if p.Discoverable != nil && *p.Discoverable != cur.Discoverable {
		next.Discoverable, changed["discoverable"] = *p.Discoverable, boolValue(*p.Discoverable)
	}
	if p.RequestPolicy != nil && *p.RequestPolicy != cur.RequestPolicy {
		next.RequestPolicy, changed["request_policy"] = *p.RequestPolicy, *p.RequestPolicy
	}
	if p.ShowStreakToFriends != nil && *p.ShowStreakToFriends != cur.ShowStreakToFriends {
		next.ShowStreakToFriends, changed["show_streak_to_friends"] = *p.ShowStreakToFriends, boolValue(*p.ShowStreakToFriends)
	}
	if p.CourseInvitesFrom != nil && *p.CourseInvitesFrom != cur.CourseInvitesFrom {
		next.CourseInvitesFrom, changed["course_invites_from"] = *p.CourseInvitesFrom, *p.CourseInvitesFrom
	}
	if len(changed) > 0 {
		if _, err = tx.Exec(ctx, `UPDATE learner_social_settings SET discoverable=$2,request_policy=$3,show_streak_to_friends=$4,course_invites_from=$5 WHERE learner_id=$1::uuid`,
			learnerID, next.Discoverable, next.RequestPolicy, next.ShowStreakToFriends, next.CourseInvitesFrom); err != nil {
			return Settings{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Settings{}, err
	}
	for _, key := range []string{"discoverable", "request_policy", "show_streak_to_friends", "course_invites_from"} {
		if value, ok := changed[key]; ok {
			s.emit(learnerID, "privacy_setting_changed", map[string]any{"setting": key, "value": value})
		}
	}
	return next, nil
}
