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

// ReplayPersonDeletionRequests retries durable requests after service restart.
func ReplayPersonDeletionRequests(ctx context.Context, pool *pgxpool.Pool, deleter PersonDeleter) error {
	if deleter == nil {
		return nil
	}
	rows, err := pool.Query(ctx, "SELECT distinct_id::text FROM posthog_person_deletion_requests ORDER BY requested_at")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range ids {
		if err = deleter.DeletePerson(ctx, id); err != nil {
			continue
		}
		if _, err = pool.Exec(ctx, "DELETE FROM posthog_person_deletion_requests WHERE distinct_id=$1::uuid", id); err != nil {
			return err
		}
	}
	return nil
}
