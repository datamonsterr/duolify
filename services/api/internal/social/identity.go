package social

import "strings"

// StreakSource reports a learner's current streak. E5-02 wires the real source;
// until then no streak is ever exposed.
type StreakSource interface {
	Streak(learnerID string) (days int, ok bool)
}

// Avatar is the default avatar; uploaded avatars arrive with the profile ticket.
type Avatar struct {
	Initial string `json:"initial"`
}

// StreakStatus is the privacy-permitted streak view.
type StreakStatus struct {
	Days int `json:"days"`
}

// Identity is the only data another learner may see: no email, profile, notes or goals.
type Identity struct {
	Username    string        `json:"username"`
	DisplayName string        `json:"display_name"`
	Avatar      Avatar        `json:"avatar"`
	Streak      *StreakStatus `json:"streak,omitempty"`
}

type identityRow struct {
	LearnerID           string
	Username            string
	DisplayName         string
	ShowStreakToFriends bool
}

// Project builds the public view. A streak needs a friend viewer, the owner's
// show_streak_to_friends setting and a streak source.
func (r identityRow) Project(streaks StreakSource, viewerIsFriend bool) Identity {
	out := Identity{Username: r.Username, DisplayName: r.DisplayName, Avatar: Avatar{Initial: strings.ToUpper(r.Username[:1])}}
	if viewerIsFriend && r.ShowStreakToFriends && streaks != nil {
		if days, ok := streaks.Streak(r.LearnerID); ok {
			out.Streak = &StreakStatus{Days: days}
		}
	}
	return out
}
