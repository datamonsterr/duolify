//go:build integration

package s3conformance

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startRustFS(t *testing.T) Config {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.Run(ctx, "rustfs/rustfs:1.0.4",
		testcontainers.WithEnv(map[string]string{"RUSTFS_ACCESS_KEY": "conf-root", "RUSTFS_SECRET_KEY": "conf-root-secret", "RUSTFS_VOLUMES": "/data", "RUSTFS_CONSOLE_ENABLE": "false"}),
		testcontainers.WithExposedPorts("9000/tcp"), testcontainers.WithCmd("/data"),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/health/ready").WithPort("9000/tcp")))
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, c)
	endpoint, err := c.PortEndpoint(ctx, "9000/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}
	return Config{Endpoint: endpoint, Region: "us-east-1", AccessKey: "conf-root", SecretKey: "conf-root-secret", PathStyle: true}
}

func TestSuitePassesAgainstRustFSAndCleansUp(t *testing.T) {
	cfg := startRustFS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	results := Run(ctx, cfg)
	for _, r := range results {
		t.Log(cfg.Redact(r.String()))
		if r.Status != StatusPass {
			t.Errorf("operation %q: %s", r.Operation, r.Detail)
		}
	}
	if len(results) != len(Operations()) {
		t.Fatalf("expected %d results, got %d", len(Operations()), len(results))
	}
	out, err := cfg.Client().ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil || len(out.Buckets) != 0 {
		t.Fatalf("suite must delete its own bucket: %v %v", out, err)
	}
}

func TestSuiteFailsNamingVersionedPutWhenVersioningIsOff(t *testing.T) {
	cfg := startRustFS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := cfg.Client()
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("unversioned")}); err != nil {
		t.Fatal(err)
	}
	cfg.Bucket = "unversioned"
	failed := map[string]bool{}
	for _, r := range Run(ctx, cfg) {
		if r.Status == StatusFail {
			failed[r.Operation] = true
		}
	}
	for _, name := range []string{"bucket versioning", "versioned put", "list versions", "get by version"} {
		if !failed[name] {
			t.Errorf("expected %q to fail on an unversioned bucket; failed=%v", name, failed)
		}
	}
	if failed["conditional create (If-None-Match: *)"] {
		t.Error("conditional create does not need versioning")
	}
	if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String("unversioned")}); err != nil {
		t.Fatal("suite must not delete a bucket it did not create")
	}
	list, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String("unversioned")})
	if err != nil || len(list.Contents) != 0 {
		t.Fatalf("suite left objects behind in a caller bucket: %v %v", list, err)
	}
}
