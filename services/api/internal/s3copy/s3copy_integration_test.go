//go:build integration

package s3copy_test

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/datamonsterr/duolify/services/api/internal/objecttest"
	"github.com/datamonsterr/duolify/services/api/internal/s3copy"
)

type entry struct {
	Marker bool
	Body   string
}

func put(t *testing.T, srv *objecttest.Server, bucket, key, body string) {
	t.Helper()
	if _, err := srv.Client.PutObject(t.Context(), &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: strings.NewReader(body), ContentType: aws.String("text/plain"), Metadata: map[string]string{"owner": "fixture"}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // S3 timestamps have one-second resolution; keep the order unambiguous.
}

func mark(t *testing.T, srv *objecttest.Server, bucket, key string) {
	t.Helper()
	if _, err := srv.Client.DeleteObject(t.Context(), &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
}

// history lists a key oldest first as body (or marker) strings.
func history(t *testing.T, srv *objecttest.Server, bucket, key string) []entry {
	t.Helper()
	out, err := srv.Client.ListObjectVersions(t.Context(), &s3.ListObjectVersionsInput{Bucket: aws.String(bucket), Prefix: aws.String(key)})
	if err != nil {
		t.Fatal(err)
	}
	type item struct {
		at time.Time
		e  entry
	}
	var items []item
	for _, v := range out.Versions {
		if aws.ToString(v.Key) != key {
			continue
		}
		obj, err := srv.Client.GetObject(t.Context(), &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), VersionId: v.VersionId})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(obj.Body)
		_ = obj.Body.Close()
		items = append(items, item{aws.ToTime(v.LastModified), entry{Body: string(raw)}})
	}
	for _, m := range out.DeleteMarkers {
		if aws.ToString(m.Key) == key {
			items = append(items, item{aws.ToTime(m.LastModified), entry{Marker: true}})
		}
	}
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			if items[j].at.Before(items[i].at) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	var res []entry
	for _, it := range items {
		res = append(res, it.e)
	}
	return res
}

func seed(t *testing.T) (*objecttest.Server, s3copy.Options) {
	srv := objecttest.Start(t)
	srv.Bucket(t, "source", true)
	srv.Bucket(t, "target", true)
	put(t, srv, "source", "a.json", "a-v1")
	put(t, srv, "source", "a.json", "a-v2-longer")
	mark(t, srv, "source", "a.json")
	put(t, srv, "source", "a.json", "a-v3")
	put(t, srv, "source", "b.json", "b-v1")
	mark(t, srv, "source", "b.json")
	put(t, srv, "source", "c.json", "c-v1")
	return srv, s3copy.Options{Src: srv.Client, SrcBucket: "source", Dst: srv.Client, DstBucket: "target", SampleSize: 1000}
}

func TestCopyKeepsEveryVersionAndDeleteMarkerAndIsResumable(t *testing.T) {
	srv, opts := seed(t)
	before := map[string][]entry{}
	for _, key := range []string{"a.json", "b.json", "c.json"} {
		before[key] = history(t, srv, "source", key)
	}
	report, err := s3copy.Run(t.Context(), opts)
	if err != nil || len(report.Mismatches) != 0 {
		t.Fatalf("copy: %v %+v", err, report)
	}
	if report.Copied != 7 {
		t.Fatalf("copied %d entries, want 7 (5 versions and 2 delete markers)", report.Copied)
	}
	for key, want := range before {
		got := history(t, srv, "target", key)
		if len(got) != len(want) {
			t.Fatalf("%s: %d entries, want %d", key, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s[%d]: %+v, want %+v", key, i, got[i], want[i])
			}
		}
		if after := history(t, srv, "source", key); len(after) != len(want) {
			t.Fatalf("source %s changed", key)
		}
	}
	head, err := srv.Client.HeadObject(t.Context(), &s3.HeadObjectInput{Bucket: aws.String("target"), Key: aws.String("c.json")})
	if err != nil || aws.ToString(head.ContentType) != "text/plain" || head.Metadata["owner"] != "fixture" {
		t.Fatalf("content type and metadata must survive: %v %+v", err, head)
	}

	again, err := s3copy.Run(t.Context(), opts)
	if err != nil || again.Copied != 0 || len(again.Mismatches) != 0 {
		t.Fatalf("second run must copy nothing and verify: %v %+v", err, again)
	}
	if bytes.Contains([]byte(again.String()), []byte(objecttest.SecretKey)) {
		t.Fatal("report leaks a credential")
	}
}

func TestVerifyFailsOnPlantedMismatch(t *testing.T) {
	srv, opts := seed(t)
	if report, err := s3copy.Run(t.Context(), opts); err != nil || len(report.Mismatches) != 0 {
		t.Fatalf("copy: %v %+v", err, report)
	}
	// Replace c.json's only target version with different bytes of the same length.
	list, err := srv.Client.ListObjectVersions(t.Context(), &s3.ListObjectVersionsInput{Bucket: aws.String("target"), Prefix: aws.String("c.json")})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range list.Versions {
		if _, err = srv.Client.DeleteObject(t.Context(), &s3.DeleteObjectInput{Bucket: aws.String("target"), Key: aws.String("c.json"), VersionId: v.VersionId}); err != nil {
			t.Fatal(err)
		}
	}
	put(t, srv, "target", "c.json", "c-v9")
	report, err := s3copy.Run(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Has(s3copy.KindChecksum) {
		t.Fatalf("same-size tamper must fail the checksum check: %+v", report)
	}
	if report.Err() == nil || !strings.Contains(report.Err().Error(), s3copy.KindChecksum) {
		t.Fatalf("error must name the mismatch: %v", report.Err())
	}

	// A lost version is a count and version-history mismatch; the source is untouched.
	srv2, opts2 := seed(t)
	if _, err = s3copy.Run(t.Context(), opts2); err != nil {
		t.Fatal(err)
	}
	list, _ = srv2.Client.ListObjectVersions(t.Context(), &s3.ListObjectVersionsInput{Bucket: aws.String("target"), Prefix: aws.String("a.json")})
	if _, err = srv2.Client.DeleteObject(t.Context(), &s3.DeleteObjectInput{Bucket: aws.String("target"), Key: aws.String("a.json"), VersionId: list.Versions[len(list.Versions)-1].VersionId}); err != nil {
		t.Fatal(err)
	}
	report, err = s3copy.Run(t.Context(), opts2)
	if err != nil || !report.Has(s3copy.KindCount) || !report.Has(s3copy.KindVersions) || !report.Has(s3copy.KindBytes) {
		t.Fatalf("lost version must be named: %v %+v", err, report)
	}
}

func TestCopyRefusesUnversionedTarget(t *testing.T) {
	srv, opts := seed(t)
	srv.Bucket(t, "plain", false)
	opts.DstBucket = "plain"
	if _, err := s3copy.Run(t.Context(), opts); err == nil {
		t.Fatal("unversioned target accepted")
	}
}
