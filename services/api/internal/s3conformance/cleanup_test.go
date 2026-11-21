package s3conformance

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// fakeStore holds one locked version; every delete fails like a key without bypass rights.
type fakeStore struct{ deleteBucketCalls int }

func (fakeStore) ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	return &s3.ListObjectVersionsOutput{Versions: []types.ObjectVersion{{Key: aws.String("locked"), VersionId: aws.String("v1")}}}, nil
}

func (fakeStore) ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return &s3.ListObjectsV2Output{}, nil
}

func (fakeStore) DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	return nil, errors.New("AccessDenied: bypass not allowed secret-key")
}

func (f *fakeStore) DeleteBucket(context.Context, *s3.DeleteBucketInput, ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
	f.deleteBucketCalls++
	return nil, errors.New("BucketNotEmpty")
}

func TestCleanupReportsLeftovers(t *testing.T) {
	warnings := cleanup(context.Background(), &fakeStore{}, "bkt", "", true)
	if len(warnings) != 1 {
		t.Fatalf("want 1 warning, got %v", warnings)
	}
	for _, want := range []string{"bkt", "locked", "v1", "AccessDenied", "BucketNotEmpty"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("warning %q lacks %q", warnings[0], want)
		}
	}
}

func TestCleanupCleanStoreHasNoWarning(t *testing.T) {
	if w := cleanup(context.Background(), emptyStore{}, "bkt", "p/", false); len(w) != 0 {
		t.Fatalf("unexpected warnings %v", w)
	}
}

type emptyStore struct{ fakeStore }

func (emptyStore) ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	return &s3.ListObjectVersionsOutput{}, nil
}
