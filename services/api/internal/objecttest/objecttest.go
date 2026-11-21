// Package objecttest starts a disposable RustFS container for object-storage tests.
package objecttest

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Credentials of the throwaway container; they protect nothing.
const (
	AccessKey = "root-synthetic"
	SecretKey = "root-synthetic-secret"
	Region    = "us-east-1"
)

// Server is one disposable S3-compatible endpoint.
type Server struct {
	Endpoint string
	Client   *s3.Client
}

// Start runs RustFS and removes it when the test ends.
func Start(t *testing.T) *Server {
	t.Helper()
	ctx := t.Context()
	container, err := testcontainers.Run(ctx, "rustfs/rustfs:1.0.4",
		testcontainers.WithEnv(map[string]string{"RUSTFS_ACCESS_KEY": AccessKey, "RUSTFS_SECRET_KEY": SecretKey, "RUSTFS_VOLUMES": "/data", "RUSTFS_CONSOLE_ENABLE": "false"}),
		testcontainers.WithExposedPorts("9000/tcp"), testcontainers.WithCmd("/data"),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/health/ready").WithPort("9000/tcp")))
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	endpoint, err := container.PortEndpoint(ctx, "9000/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}
	return &Server{Endpoint: endpoint, Client: Client(endpoint)}
}

// Client builds a path-style client with the test credentials.
func Client(endpoint string) *s3.Client {
	return s3.New(s3.Options{Region: Region, BaseEndpoint: aws.String(endpoint), UsePathStyle: true,
		Credentials:                credentials.NewStaticCredentialsProvider(AccessKey, SecretKey, ""),
		RetryMaxAttempts:           1,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired})
}

// Bucket creates a bucket, versioned when requested.
func (s *Server) Bucket(t *testing.T, name string, versioned bool) {
	t.Helper()
	if _, err := s.Client.CreateBucket(t.Context(), &s3.CreateBucketInput{Bucket: aws.String(name)}); err != nil {
		t.Fatal(err)
	}
	if !versioned {
		return
	}
	if _, err := s.Client.PutBucketVersioning(t.Context(), &s3.PutBucketVersioningInput{Bucket: aws.String(name), VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}}); err != nil {
		t.Fatal(err)
	}
}
