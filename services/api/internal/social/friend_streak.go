package social

import (
	"context"
	"time"
)

// EvaluateFriendStreaks settles every Friend streak of the learner. (E6-06 stub.)
func (s *Service) EvaluateFriendStreaks(ctx context.Context, learnerID string) error { return nil }

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}
