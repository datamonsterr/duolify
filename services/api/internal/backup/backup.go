// Package backup encrypts database archives before publishing them to object storage.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"

	"filippo.io/age"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
)

// Source isolates the database and pg_dump process.
type Source interface {
	SchemaVersion(context.Context) (int64, error)
	Dump(context.Context, io.Writer) error
}

// Objects supports complete atomic writes only. It deliberately has no delete API.
type Objects interface {
	Exists(context.Context, string) (bool, error)
	Put(context.Context, string, io.ReadSeeker, int64) error
}

// Options supplies external boundaries and the public encryption recipient.
type Options struct {
	Source    Source
	Objects   Objects
	Recipient string
	Events    analytics.Events
	OptOut    bool
	TempDir   string
}

// Service backs up a database without ever writing a plaintext archive to disk.
type Service struct {
	options   Options
	recipient *age.X25519Recipient
}

// New validates the key before any database or object operation.
func New(options Options) (*Service, error) {
	recipient, err := age.ParseX25519Recipient(options.Recipient)
	if err != nil {
		return nil, errors.New("invalid BACKUP_AGE_RECIPIENT")
	}
	if options.Source == nil || options.Objects == nil {
		return nil, errors.New("backup source and objects are required")
	}
	return &Service{options: options, recipient: recipient}, nil
}

// Run uses a durable date supplied by River, so retries do not drift at midnight.
func (s *Service) Run(ctx context.Context, date string) (result error) {
	started := time.Now()
	var size int64
	defer func() { s.emit(result == nil, size, time.Since(started)) }()
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return errors.New("invalid backup UTC date")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	version, err := s.options.Source.SchemaVersion(ctx)
	if err != nil || version < 1 {
		return errors.New("backup schema check failed")
	}
	key := fmt.Sprintf("%s/schema-%d.dump.age", date, version)
	exists, err := s.options.Objects.Exists(ctx, key)
	if err != nil {
		return errors.New("backup object check failed")
	}
	if exists {
		return nil
	}
	file, err := os.CreateTemp(s.options.TempDir, "duolify-backup-*.age")
	if err != nil {
		return errors.New("backup temporary file failed")
	}
	defer func() {
		_ = file.Close()
		if err := os.Remove(file.Name()); err != nil {
			result = errors.Join(result, errors.New("backup temporary cleanup failed"))
		}
	}()
	// Bound encrypted output below S3's single-PutObject 5 GiB limit. Never
	// initiate multipart uploads, so failures cannot leave incomplete uploads.
	encrypted, err := age.Encrypt(&limitedWriter{Writer: file, remaining: 4 << 30}, s.recipient)
	if err != nil {
		return errors.New("backup encryption failed")
	}
	if err := s.options.Source.Dump(ctx, encrypted); err != nil {
		return errors.New("backup dump failed")
	}
	if err := encrypted.Close(); err != nil {
		return errors.New("backup encryption failed")
	}
	// A concurrent migration must not publish a snapshot under the old schema
	// version. Retry against the settled schema instead of mislabeling recovery.
	endingVersion, err := s.options.Source.SchemaVersion(ctx)
	if err != nil || endingVersion != version {
		return errors.New("backup schema changed during dump")
	}
	size, err = file.Seek(0, io.SeekEnd)
	if err != nil {
		return errors.New("backup file size failed")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return errors.New("backup file rewind failed")
	}
	if err := ctx.Err(); err != nil {
		return errors.New("backup deadline exceeded")
	}
	if err := s.options.Objects.Put(ctx, key, file, size); err != nil {
		return errors.New("backup upload failed")
	}
	return nil
}

type limitedWriter struct {
	io.Writer
	remaining int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, errors.New("backup exceeds single upload limit")
	}
	n, err := w.Writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}

func (s *Service) emit(ok bool, size int64, duration time.Duration) {
	if s.options.OptOut || s.options.Events == nil {
		return
	}
	name := "backup_failed"
	if ok {
		name = "backup_completed"
	}
	version := "development"
	if info, present := debug.ReadBuildInfo(); present && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	_ = s.options.Events.Capture("00000000-0000-4000-8000-000000000011", name, map[string]any{
		"source": "api", "platform": "web", "app_version": version,
		"size_bytes": size, "duration_ms": duration.Milliseconds(), "ok": ok,
	})
}
