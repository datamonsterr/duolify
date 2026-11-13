package player

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3 uses the application's existing credentials, not backup-writer privileges.
type S3 struct {
	Client *s3.Client
	Bucket string
}

// NewS3 uses explicit application credentials and bounded S3 retries.
func NewS3(getenv func(string) string) *S3 {
	return &S3{Bucket: getenv("S3_BUCKET"), Client: s3.New(s3.Options{Region: getenv("S3_REGION"), BaseEndpoint: aws.String(getenv("S3_ENDPOINT")), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider(getenv("S3_ACCESS_KEY_ID"), getenv("S3_SECRET_ACCESS_KEY"), ""), RetryMaxAttempts: 1, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired})}
}

// Get reads a bounded immutable lesson object.
func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	if !validKey(key) {
		return nil, errInvalid
	}
	out, err := s.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
	if err != nil {
		return nil, errors.New("lesson object unavailable")
	}
	defer func() { _ = out.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(out.Body, maxObjectBytes+1))
	if err != nil || len(raw) > maxObjectBytes {
		return nil, errInvalid
	}
	return raw, nil
}

// Put publishes immutable keys; bucket versioning and protected copies are managed
// by the storage operator under the data-backup policy.
func (s *S3) Put(ctx context.Context, key string, raw []byte) error {
	if !validKey(key) || len(raw) > maxObjectBytes {
		return errInvalid
	}
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key), Body: bytes.NewReader(raw), IfNoneMatch: aws.String("*")})
	if err != nil {
		var response interface{ HTTPStatusCode() int }
		if errors.As(err, &response) && response.HTTPStatusCode() == 412 {
			existing, readErr := s.Get(ctx, key)
			if readErr == nil && bytes.Equal(existing, raw) {
				return nil
			}
		}
		return errors.New("lesson object publication failed")
	}
	return nil
}
