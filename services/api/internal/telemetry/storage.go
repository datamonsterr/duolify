package telemetry

import (
	"context"
	"errors"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// StorageProbe checks authenticated access to the application bucket only.
// Misconfiguration stays unhealthy without logging credentials or endpoints.
func StorageProbe(getenv func(string) string) func(context.Context) error {
	endpoint, err := url.Parse(getenv("S3_ENDPOINT"))
	bucket := getenv("S3_BUCKET")
	region := getenv("S3_REGION")
	key := getenv("S3_ACCESS_KEY_ID")
	secret := getenv("S3_SECRET_ACCESS_KEY")
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || bucket == "" || region == "" || key == "" || secret == "" {
		return func(context.Context) error { return errors.New("object storage configuration unavailable") }
	}
	client := s3.New(s3.Options{Region: region, BaseEndpoint: aws.String(endpoint.String()), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider(key, secret, ""), RetryMaxAttempts: 1})
	return func(ctx context.Context) error {
		_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
		if err != nil {
			return errors.New("object storage unavailable")
		}
		return nil
	}
}
