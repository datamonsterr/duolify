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
)

// S3Options deliberately requires dedicated credentials; no default credential
// chain can accidentally select the application's or a host administrator's key.
type S3Options struct{ Endpoint, Region, Bucket, ApplicationBucket, AccessKey, SecretKey string }

// S3 publishes complete objects with a single conditional PutObject.
type S3 struct {
	Client *s3.Client
	Bucket string
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
	return &S3{Bucket: options.Bucket, Client: s3.New(s3.Options{
		Region: options.Region, BaseEndpoint: aws.String(options.Endpoint), UsePathStyle: true,
		Credentials:                credentials.NewStaticCredentialsProvider(options.AccessKey, options.SecretKey, ""),
		RetryMaxAttempts:           1,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
	})}, nil
}

// Exists only treats an explicit NotFound as an absent object.
func (s *S3) Exists(ctx context.Context, key string) (bool, error) {
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
