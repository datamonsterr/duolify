package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PersonDeleter requests deletion of the Learner UUID and all related events.
type PersonDeleter interface {
	DeletePerson(context.Context, string) error
}

type posthogPersonDeleter struct {
	host      string
	projectID string
	apiKey    string
	client    *http.Client
}

// NewPostHogPersonDeleter returns nil until the project owner provisions the
// private API credential. Public ingestion keys are deliberately not accepted.
func NewPostHogPersonDeleter(host, projectID, apiKey string) PersonDeleter {
	if projectID == "" || apiKey == "" {
		return nil
	}
	return &posthogPersonDeleter{
		host:      strings.TrimRight(host, "/"),
		projectID: url.PathEscape(projectID),
		apiKey:    apiKey,
		client:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (d *posthogPersonDeleter) DeletePerson(ctx context.Context, learnerID string) error {
	body, err := json.Marshal(map[string]any{
		"distinct_ids":      []string{learnerID},
		"delete_events":     true,
		"delete_recordings": true,
	})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/api/projects/%s/persons/bulk_delete/", d.host, d.projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	req.Header.Set("Content-Type", "application/json")
	response, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusAccepted {
		return errors.New("PostHog did not accept person deletion")
	}
	var result struct {
		Errors []json.RawMessage `json:"deletion_errors"`
	}
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		return err
	}
	if len(result.Errors) > 0 {
		return errors.New("PostHog reported person deletion errors")
	}
	return nil
}

// ConfigurePersonDeletion injects the optional, private PostHog deletion API.
func (s *Service) ConfigurePersonDeletion(deleter PersonDeleter) {
	s.personDeleter = deleter
}

// ErrPersonNotFound is what a PersonDeleter returns when PostHog has no such person. The person
// is already gone, which is the goal state, so the request counts as done.
var ErrPersonNotFound = errors.New("posthog person not found")

// PostHogPersonDeleterFromEnv returns nil unless the project id and the personal API key (a
// private key, never the public project key) are both set, so PostHog is never called without
// them. The private API lives on the app host (us.posthog.com), not the ingestion host.
func PostHogPersonDeleterFromEnv(getenv func(string) string) PersonDeleter {
	host := getenv("POSTHOG_HOST")
	if host == "" {
		host = "https://us.i.posthog.com"
	}
	return NewPostHogPersonDeleter(analytics.AppHost(host), getenv("POSTHOG_PROJECT_ID"), getenv("POSTHOG_PERSONAL_API_KEY"))
}

// PostHogPersonEraser drains posthog_person_deletion_requests. Like ClerkUserEraser it is
// independent of the HTTP Service: the River worker builds it from a pool and the optional deleter.
type PostHogPersonEraser struct {
	pool    *pgxpool.Pool
	deleter PersonDeleter
	timeout time.Duration
}

// NewPostHogPersonEraser returns an eraser; a nil deleter disables it entirely.
func NewPostHogPersonEraser(pool *pgxpool.Pool, deleter PersonDeleter) *PostHogPersonEraser {
	return &PostHogPersonEraser{pool: pool, deleter: deleter, timeout: 10 * time.Second}
}

// PersonDeletionEnabled reports whether PostHog deletion is configured. Without it nothing is
// scheduled or called, and queued requests wait until it is.
func (e *PostHogPersonEraser) PersonDeletionEnabled() bool { return e != nil && e.deleter != nil }

const personDeletionBatch = 50

// ProcessPersonDeletions deletes queued PostHog persons. A request leaves the queue only after
// PostHog accepted it or reported the person already gone; any other failure keeps it (with an
// attempt count) for the next run. It returns how many were deleted and an error carrying counts
// only: never a learner id.
func (e *PostHogPersonEraser) ProcessPersonDeletions(ctx context.Context) (int, error) {
	if !e.PersonDeletionEnabled() {
		return 0, nil
	}
	rows, err := e.pool.Query(ctx, "SELECT distinct_id::text FROM posthog_person_deletion_requests ORDER BY requested_at LIMIT $1", personDeletionBatch)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	done, failed := 0, 0
	for _, id := range ids {
		callCtx, cancel := context.WithTimeout(ctx, e.timeout)
		err = e.deleter.DeletePerson(callCtx, id)
		cancel()
		if err != nil && !errors.Is(err, ErrPersonNotFound) {
			failed++
			if _, dbErr := e.pool.Exec(ctx, "UPDATE posthog_person_deletion_requests SET attempts=attempts+1,last_attempt_at=now() WHERE distinct_id=$1::uuid", id); dbErr != nil {
				return done, dbErr
			}
			continue
		}
		if _, err = e.pool.Exec(ctx, "DELETE FROM posthog_person_deletion_requests WHERE distinct_id=$1::uuid", id); err != nil {
			return done, err
		}
		done++
	}
	if failed > 0 {
		return done, fmt.Errorf("%d of %d posthog person deletions failed and stay queued", failed, len(ids))
	}
	return done, nil
}
