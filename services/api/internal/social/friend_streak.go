package social

import (
	"context"
	"time"
)

// DaySide is one friend's own local day as shown in the Friends list.
type DaySide struct {
	LocalDate string `json:"local_date"`
	TimeZone  string `json:"time_zone"`
	Done      bool   `json:"done"`
	Protected bool   `json:"protected"`
}

// FriendStreakView is the privacy-permitted shared streak: counts, state and both local days.
type FriendStreakView struct {
	Days          int        `json:"days"`
	Longest       int        `json:"longest"`
	State         string     `json:"state"`
	You           DaySide    `json:"you"`
	Friend        DaySide    `json:"friend"`
	CheckingUntil *time.Time `json:"checking_until,omitempty"`
}

type sideDays struct {
	Zone              string
	Active, Protected map[string]bool
	Decided           string
}

type runState struct {
	Days, Longest, Run                         int
	CursorLow, CursorHigh, StartLow, StartHigh string
}

type settledDay struct {
	Low, High, Outcome string
	Length, Run        int
	Started            bool
}

func localDayBounds(date, zone string) (start, end time.Time, err error) { return }

func datesOverlap(lowZone, lowDate, highZone, highDate string) bool { return false }

func evaluatePair(st runState, low, high sideDays, since time.Time) (runState, []settledDay) {
	return st, nil
}

func friendStreakView(st runState, you, friend sideDays, youIsLow bool, now time.Time) FriendStreakView {
	return FriendStreakView{}
}

// EvaluateFriendStreaks settles every Friend streak of the learner. (E6-06 stub.)
func (s *Service) EvaluateFriendStreaks(ctx context.Context, learnerID string) error { return nil }

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}
