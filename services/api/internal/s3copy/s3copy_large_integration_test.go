//go:build integration

package s3copy_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/datamonsterr/duolify/services/api/internal/objecttest"
	"github.com/datamonsterr/duolify/services/api/internal/s3copy"
)

// recordingTarget records the largest request body and the multipart calls the copy makes.
type recordingTarget struct {
	*s3.Client
	mu        sync.Mutex
	maxBody   int
	multipart int
	puts      int
}

func (r *recordingTarget) note(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maxBody = max(r.maxBody, n)
}

func (r *recordingTarget) PutObject(ctx context.Context, in *s3.PutObjectInput, fns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	raw, _ := io.ReadAll(in.Body)
	in.Body = bytes.NewReader(raw)
	r.note(len(raw))
	r.mu.Lock()
	r.puts++
	r.mu.Unlock()
	return r.Client.PutObject(ctx, in, fns...)
}

func (r *recordingTarget) CreateMultipartUpload(ctx context.Context, in *s3.CreateMultipartUploadInput, fns ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
	r.mu.Lock()
	r.multipart++
	r.mu.Unlock()
	return r.Client.CreateMultipartUpload(ctx, in, fns...)
}

func (r *recordingTarget) UploadPart(ctx context.Context, in *s3.UploadPartInput, fns ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
	raw, _ := io.ReadAll(in.Body)
	in.Body = bytes.NewReader(raw)
	r.note(len(raw))
	return r.Client.UploadPart(ctx, in, fns...)
}

func digestOf(t *testing.T, srv *objecttest.Server, bucket, key string) [sha256.Size]byte {
	t.Helper()
	out, err := srv.Client.GetObject(t.Context(), &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Body.Close() }()
	h := sha256.New()
	if _, err = io.Copy(h, out.Body); err != nil {
		t.Fatal(err)
	}
	return [sha256.Size]byte(h.Sum(nil))
}

// An object bigger than one part is copied as a multipart upload with at most one part
// buffered, keeps its content type and metadata, and passes the full checksum verification.
func TestCopyStreamsLargeObjectsInParts(t *testing.T) {
	const part = 5 << 20
	srv := objecttest.Start(t)
	srv.Bucket(t, "source", true)
	srv.Bucket(t, "target", true)
	body := make([]byte, 2*part+part/2)
	for i := range body {
		body[i] = byte(i * 31 >> 3)
	}
	if _, err := srv.Client.PutObject(t.Context(), &s3.PutObjectInput{Bucket: aws.String("source"), Key: aws.String("dump.enc"), Body: bytes.NewReader(body),
		ContentType: aws.String("application/octet-stream"), Metadata: map[string]string{"owner": "fixture"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Client.PutObject(t.Context(), &s3.PutObjectInput{Bucket: aws.String("source"), Key: aws.String("small.json"), Body: bytes.NewReader([]byte("{}"))}); err != nil {
		t.Fatal(err)
	}
	dst := &recordingTarget{Client: srv.Client}
	report, err := s3copy.Run(t.Context(), s3copy.Options{Src: srv.Client, SrcBucket: "source", Dst: dst, DstBucket: "target", PartSize: part})
	if err != nil || len(report.Mismatches) != 0 {
		t.Fatalf("copy: %v %+v", err, report)
	}
	if dst.multipart != 1 || dst.maxBody > part {
		t.Fatalf("want one multipart upload with parts <= %d bytes, got %d uploads, largest body %d", part, dst.multipart, dst.maxBody)
	}
	if digestOf(t, srv, "source", "dump.enc") != digestOf(t, srv, "target", "dump.enc") {
		t.Fatal("copied bytes differ")
	}
	head, err := srv.Client.HeadObject(t.Context(), &s3.HeadObjectInput{Bucket: aws.String("target"), Key: aws.String("dump.enc")})
	if err != nil || aws.ToString(head.ContentType) != "application/octet-stream" || head.Metadata["owner"] != "fixture" {
		t.Fatalf("content type or metadata lost: %v %+v", err, head)
	}
	if report.ChecksumsVerified != 2 {
		t.Fatalf("every version must be checksummed by default: %+v", report)
	}
}
