package social

import (
	"context"
	"time"
)

const (
	bucketSearch  = "search"
	bucketAttempt = "username_attempt"
	searchWindow  = time.Minute
	attemptWindow = time.Hour
)

// allow counts one hit in the learner's fixed window and refuses once the
// window holds more than limit. One atomic upsert, so concurrent requests on any
// API instance share the same count. A boundary can admit up to 2x limit briefly.
func (s *Service) allow(ctx context.Context, learnerID, bucket string, limit int, w time.Duration) (*Rejection, error) {
	var count int
	var left float64
	err := s.pool.QueryRow(ctx, `INSERT INTO social_rate_limits(learner_id,bucket,window_start,count) VALUES($1::uuid,$2,now(),1)
 ON CONFLICT (learner_id,bucket) DO UPDATE SET
  count=CASE WHEN social_rate_limits.window_start<=now()-make_interval(secs=>$3) THEN 1 ELSE LEAST(social_rate_limits.count+1,$4+1) END,
  window_start=CASE WHEN social_rate_limits.window_start<=now()-make_interval(secs=>$3) THEN now() ELSE social_rate_limits.window_start END
 RETURNING count, EXTRACT(EPOCH FROM (window_start+make_interval(secs=>$3)-now()))::float8`, learnerID, bucket, w.Seconds(), limit).Scan(&count, &left)
	if err != nil {
		return nil, err
	}
	if count > limit {
		return &Rejection{Code: CodeRateLimited, RetryAfter: time.Duration(left * float64(time.Second))}, nil
	}
	return nil, nil
}
