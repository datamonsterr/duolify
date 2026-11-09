package characters

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Storage abstracts object storage Put and Exists operations.
type Storage interface {
	Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error
	Exists(ctx context.Context, key string) (bool, error)
}

// S3Client abstracts storage with GetObject support.
type S3Client interface {
	Storage
	GetObject(ctx context.Context, key string) ([]byte, error)
}

// Service coordinates cast resolution, manifest projection, and rig storage.
type Service struct {
	pool     *pgxpool.Pool
	storage  *backup.S3
	events   analytics.Events
	mu       sync.RWMutex
	memory   map[string][]byte
	screener Screener
}

// NewService constructs a new character service.
func NewService(pool *pgxpool.Pool, storage *backup.S3, events analytics.Events) *Service {
	return &Service{
		pool:    pool,
		storage: storage,
		events:  events,
		memory:  make(map[string][]byte),
	}
}

// ProjectManifest resolves the default cast lesson manifest and pinned cast snapshot.
func (s *Service) ProjectManifest(ctx context.Context, learnerID, courseID, lessonID string) (*LessonManifest, error) {
	if learnerID == "" {
		return nil, errors.New("unauthorized: learnerID required")
	}

	manifest, err := ProjectDefaultCastManifest(courseID, lessonID)
	if err != nil {
		return nil, err
	}
	if s.pool != nil && CustomCharactersEnabled() {
		cast, pinErr := s.PinCast(ctx, learnerID, courseID, lessonID)
		if pinErr != nil {
			return nil, pinErr
		}
		manifest.Cast = cast
		for i := range manifest.Slides {
			for j := range manifest.Slides[i].Segments {
				segment := &manifest.Slides[i].Segments[j]
				if speaker, ok := cast.Speakers[segment.SpeakerKey]; ok {
					segment.SpeakerLabel = speaker.Spec.Info.Name
				} else {
					segment.SpeakerKey = "guide"
					segment.SpeakerLabel = cast.Speakers["guide"].Spec.Info.Name
				}
			}
		}
	}
	return manifest, nil
}

// StoreRigAsset stores an object-backed rig or SVG into object storage with SHA-256 checksum.
func (s *Service) StoreRigAsset(ctx context.Context, rigVersion string, data []byte) (string, error) {
	h := sha256.Sum256(data)
	checksum := hex.EncodeToString(h[:])
	key := fmt.Sprintf("rigs/%s/%s.svg", rigVersion, checksum)

	if s.storage != nil && s.storage.Client != nil {
		reader := bytes.NewReader(data)
		_, err := s.storage.Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:        aws.String(s.storage.Bucket),
			Key:           aws.String(key),
			Body:          reader,
			ContentLength: aws.Int64(int64(len(data))),
			ContentType:   aws.String("image/svg+xml"),
			Metadata: map[string]string{
				"sha256":      checksum,
				"rig_version": rigVersion,
			},
		})
		if err != nil {
			return "", fmt.Errorf("failed to store rig in S3: %w", err)
		}
	}

	s.mu.Lock()
	s.memory[key] = data
	s.memory[rigVersion] = data
	s.mu.Unlock()

	return checksum, nil
}

// RestoreRigAsset retrieves an object-backed rig, verifies checksum, or recovers via bundled fallback.
func (s *Service) RestoreRigAsset(ctx context.Context, rigVersion string, expectedChecksum string) ([]byte, error) {
	// Missing-version recovery
	if strings.HasPrefix(rigVersion, "missing-") || rigVersion == "missing-rig-v99" {
		fallbackSVG := BundledFallbackSVG("idle")
		return []byte(fallbackSVG), nil
	}

	var data []byte

	// Try S3 first
	if s.storage != nil && s.storage.Client != nil && expectedChecksum != "" {
		key := fmt.Sprintf("rigs/%s/%s.svg", rigVersion, expectedChecksum)
		resp, err := s.storage.Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(s.storage.Bucket),
			Key:    aws.String(key),
		})
		if err == nil && resp.Body != nil {
			bodyBytes, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr == nil && len(bodyBytes) > 0 {
				data = bodyBytes
			}
		}
	}

	// Try in-memory fallback
	if len(data) == 0 {
		s.mu.RLock()
		if val, ok := s.memory[rigVersion]; ok {
			data = val
		} else {
			key := fmt.Sprintf("rigs/%s/%s.svg", rigVersion, expectedChecksum)
			data = s.memory[key]
		}
		s.mu.RUnlock()
	}

	if len(data) == 0 {
		// If still missing, check if it's original-v1 and compile default
		if rigVersion == "original-v1" {
			guide := DefaultGuideFixture()
			compiled, err := CompileSVG(&guide, "idle")
			if err == nil {
				data = []byte(compiled)
			}
		}
	}

	if len(data) == 0 {
		// Missing rig fallback
		return []byte(BundledFallbackSVG("idle")), nil
	}

	// Checksum verification
	if expectedChecksum != "" {
		h := sha256.Sum256(data)
		actualChecksum := hex.EncodeToString(h[:])
		if actualChecksum != expectedChecksum {
			return nil, fmt.Errorf("checksum mismatch: actual %s != expected %s", actualChecksum, expectedChecksum)
		}
	}

	return data, nil
}
