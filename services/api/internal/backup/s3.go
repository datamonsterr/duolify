package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/datamonsterr/duolify/services/api/internal/s3fallback"
)

// S3Options deliberately requires dedicated credentials; no default credential
// chain can accidentally select the application's or a host administrator's key.
type S3Options struct {
	Endpoint, Region, Bucket, ApplicationBucket, AccessKey, SecretKey string
	// Fallback is the temporary read-only old store (ADR 0014); nil unless enabled.
	Fallback *s3fallback.Store
}

// S3 publishes complete objects with a single conditional PutObject.
type S3 struct {
	Client *s3.Client
	Bucket string
	// Old serves reads the new store misses during migration; writes never reach it.
	Old *S3
}

// NewS3 validates explicit storage configuration. Local RustFS uses HTTP.
func NewS3(options S3Options) (*S3, error) {
	endpoint, err := url.Parse(options.Endpoint)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("invalid BACKUP_S3_ENDPOINT")
	}
	if options.Bucket == "" || options.Bucket == options.ApplicationBucket || options.Region == "" || options.AccessKey == "" || options.SecretKey == "" {
		return nil, errors.New("dedicated backup bucket, region and writer credentials required")
	}
	store := &S3{Bucket: options.Bucket, Client: newClient(options.Endpoint, options.Region, options.AccessKey, options.SecretKey)}
	if old := options.Fallback; old != nil {
		store.Old = &S3{Bucket: old.Bucket, Client: newClient(old.Endpoint, old.Region, old.AccessKey, old.SecretKey)}
	}
	return store, nil
}

func newClient(endpoint, region, accessKey, secretKey string) *s3.Client {
	return s3.New(s3.Options{
		Region: region, BaseEndpoint: aws.String(endpoint), UsePathStyle: true,
		Credentials:                credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		RetryMaxAttempts:           1,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
	})
}

// Exists only treats an explicit NotFound as an absent object.
func (s *S3) Exists(ctx context.Context, key string) (bool, error) {
	ok, err := s.exists(ctx, key)
	if !ok && s.Old != nil {
		if oldOK, oldErr := s.Old.exists(ctx, key); oldErr == nil && oldOK {
			return true, nil
		}
	}
	return ok, err
}

func (s *S3) exists(ctx context.Context, key string) (bool, error) {
	_, err := s.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
	if err == nil {
		return true, nil
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) && (apiError.ErrorCode() == "NotFound" || apiError.ErrorCode() == "NoSuchKey") {
		return false, nil
	}
	return false, fmt.Errorf("backup head failed")
}

// Put commits only a complete encrypted body and preserves any concurrent winner.
func (s *S3) Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key), Body: body, ContentLength: aws.Int64(size), ContentType: aws.String("application/octet-stream"), IfNoneMatch: aws.String("*")})
	if err != nil {
		var responseError interface{ HTTPStatusCode() int }
		if errors.As(err, &responseError) && responseError.HTTPStatusCode() == http.StatusPreconditionFailed {
			return nil
		}
		return errors.New("backup put failed")
	}
	return nil
}

// List returns keys under a prefix; used only by the separate expirer identity.
func (s *S3) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	pages := s3.NewListObjectsV2Paginator(s.Client, &s3.ListObjectsV2Input{Bucket: aws.String(s.Bucket), Prefix: aws.String(prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, errors.New("backup list failed")
		}
		for _, o := range page.Contents {
			out = append(out, Object{Key: aws.ToString(o.Key), Size: aws.ToInt64(o.Size)})
		}
	}
	return out, nil
}

// Delete removes one object; only the expirer policy allows this.
func (s *S3) Delete(ctx context.Context, key string) error {
	if _, err := s.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)}); err != nil {
		return errors.New("backup delete failed")
	}
	return nil
}
