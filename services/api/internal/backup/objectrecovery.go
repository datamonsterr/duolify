package backup

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"time"
)

// Version is one stored version of an application object.
type Version struct {
	ID           string
	Latest       bool
	DeleteMarker bool
	Size         int64
	Modified     time.Time
	Metadata     map[string]string
}

// VersionedObjects is the operator-only surface over the versioned application bucket.
type VersionedObjects interface {
	ListVersions(context.Context, string) ([]Version, error)
	GetVersion(context.Context, string, string) ([]byte, error)
	PutVersioned(context.Context, string, []byte, map[string]string) (string, error)
}

var jobID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{7,63}$`)

// Recovery restores or regenerates an application object only as a new version
// under the same key, so every stored reference keeps working and no history is
// removed. Each write is tied to an operator job id; nothing happens silently.
type Recovery struct {
	Objects VersionedObjects
	Logger  *slog.Logger
	Events  interface {
		Capture(string, string, map[string]any) error
	}
	OptOut bool
}

// RestoreVersion re-publishes a previous, non-deleted version as the newest one.
func (r Recovery) RestoreVersion(ctx context.Context, key, versionID, job string) (newID string, err error) {
	if !jobID.MatchString(job) || key == "" || versionID == "" || r.Objects == nil {
		return "", errors.New("tracked recovery job id, key and version required")
	}
	started := time.Now()
	var size int64
	defer func() {
		if err != nil {
			emitOutcome(r.Events, r.OptOut, false, size, time.Since(started))
		}
	}()
	versions, listErr := r.Objects.ListVersions(ctx, key)
	if listErr != nil {
		return "", errors.New("object recovery listing failed")
	}
	found := false
	for _, v := range versions {
		if v.ID == versionID && !v.DeleteMarker && !v.Latest {
			found = true
		}
	}
	if !found {
		return "", errors.New("recoverable version not found")
	}
	body, getErr := r.Objects.GetVersion(ctx, key, versionID)
	if getErr != nil {
		return "", errors.New("object recovery read failed")
	}
	size = int64(len(body))
	return r.write(ctx, key, body, job, "restore", versionID)
}

// Regenerate stores operator-generated replacement bytes as a new tracked version.
func (r Recovery) Regenerate(ctx context.Context, key, job string, produce func(context.Context) ([]byte, error)) (string, error) {
	if !jobID.MatchString(job) || key == "" || produce == nil || r.Objects == nil {
		return "", errors.New("tracked regeneration job id, key and generator required")
	}
	body, err := produce(ctx)
	if err != nil || len(body) == 0 {
		emitOutcome(r.Events, r.OptOut, false, 0, 0)
		return "", errors.New("object regeneration failed")
	}
	return r.write(ctx, key, body, job, "regenerate", "")
}

func (r Recovery) write(ctx context.Context, key string, body []byte, job, kind, source string) (string, error) {
	started := time.Now()
	meta := map[string]string{"duolify-recovery-job": job, "duolify-recovery-kind": kind}
	if source != "" {
		meta["duolify-recovery-source-version"] = source
	}
	id, err := r.Objects.PutVersioned(ctx, key, body, meta)
	if err != nil {
		emitOutcome(r.Events, r.OptOut, false, int64(len(body)), time.Since(started))
		return "", errors.New("object recovery write failed")
	}
	if r.Logger != nil {
		r.Logger.InfoContext(ctx, "object recovery completed", "job", job, "kind", kind, "key", key, "source_version", source, "new_version", id, "bytes", len(body))
	}
	return id, nil
}
