// Package s3copy copies every object version and delete marker from one bucket to another
// and verifies the result (ADR 0014, E18-06). The source is only ever read: its type has no
// write method, so the tool cannot modify or delete it.
package s3copy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Mismatch kinds, named in the failure message.
const (
	KindCount    = "count"
	KindBytes    = "bytes"
	KindChecksum = "checksum"
	KindVersions = "versions"
)

const (
	// Copies buffer one object in memory because PutObject needs a seekable body.
	maxObjectBytes = 512 << 20
	// sourceVersionMeta records which source version a target version came from.
	sourceVersionMeta = "src-version-id"
)

// Reader is all the source needs.
type Reader interface {
	ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	GetBucketVersioning(context.Context, *s3.GetBucketVersioningInput, ...func(*s3.Options)) (*s3.GetBucketVersioningOutput, error)
}

// Writer is the target: readable and writable.
type Writer interface {
	Reader
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// Options selects the two buckets. SampleSize caps how many versions are checksummed
// (evenly spaced); zero or less means all of them.
type Options struct {
	Src        Reader
	SrcBucket  string
	Dst        Writer
	DstBucket  string
	SampleSize int
}

// Mismatch names one failed check.
type Mismatch struct{ Kind, Key, Detail string }

// Report is the outcome of one run.
type Report struct {
	Copied            int
	SrcEntries        int
	DstEntries        int
	SrcBytes          int64
	DstBytes          int64
	ChecksumsVerified int
	Mismatches        []Mismatch
}

// Has reports whether a mismatch of the kind was found.
func (r *Report) Has(kind string) bool {
	for _, m := range r.Mismatches {
		if m.Kind == kind {
			return true
		}
	}
	return false
}

// Err names every mismatch kind, or is nil when the copy verified.
func (r *Report) Err() error {
	if len(r.Mismatches) == 0 {
		return nil
	}
	kinds := map[string]bool{}
	var names []string
	for _, m := range r.Mismatches {
		if !kinds[m.Kind] {
			kinds[m.Kind] = true
			names = append(names, m.Kind)
		}
	}
	sort.Strings(names)
	return fmt.Errorf("verification failed: %s mismatch", strings.Join(names, ", "))
}

// String is a credential-free summary.
func (r *Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "copied %d entries; source %d entries / %d bytes; target %d entries / %d bytes; %d checksums verified; %d mismatches\n", r.Copied, r.SrcEntries, r.SrcBytes, r.DstEntries, r.DstBytes, r.ChecksumsVerified, len(r.Mismatches))
	for _, m := range r.Mismatches {
		fmt.Fprintf(&b, "MISMATCH %s key=%q %s\n", m.Kind, m.Key, m.Detail)
	}
	return b.String()
}

type entry struct {
	Marker   bool
	Version  string
	Size     int64
	Modified time.Time
	Latest   bool
}

// Run copies what the target lacks, oldest first per key, then verifies. A non-nil error
// is an operational failure; verification failures are in Report.Mismatches (see Report.Err).
func Run(ctx context.Context, o Options) (*Report, error) {
	state, err := o.Dst.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(o.DstBucket)})
	if err != nil {
		return nil, errors.New("target bucket versioning check failed")
	}
	if state.Status != types.BucketVersioningStatusEnabled {
		return nil, errors.New("target bucket must have versioning enabled")
	}
	src, err := list(ctx, o.Src, o.SrcBucket)
	if err != nil {
		return nil, fmt.Errorf("list source: %w", err)
	}
	dst, err := list(ctx, o.Dst, o.DstBucket)
	if err != nil {
		return nil, fmt.Errorf("list target: %w", err)
	}
	report := &Report{}
	for _, key := range sortedKeys(src) {
		have := dst[key]
		if len(have) > len(src[key]) || !samePrefix(src[key], have) {
			report.Mismatches = append(report.Mismatches, Mismatch{KindVersions, key, "target history is not a prefix of the source history; not modified"})
			continue
		}
		for _, e := range src[key][len(have):] {
			if err = copyEntry(ctx, o, key, e); err != nil {
				return report, err
			}
			report.Copied++
		}
	}
	if dst, err = list(ctx, o.Dst, o.DstBucket); err != nil {
		return report, fmt.Errorf("list target: %w", err)
	}
	return report, verify(ctx, o, src, dst, report)
}

func copyEntry(ctx context.Context, o Options, key string, e entry) error {
	if e.Marker {
		if _, err := o.Dst.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(o.DstBucket), Key: aws.String(key)}); err != nil {
			return fmt.Errorf("write delete marker %q failed", key)
		}
		return nil
	}
	if e.Size > maxObjectBytes {
		return fmt.Errorf("object %q is too large for a buffered copy", key)
	}
	out, err := o.Src.GetObject(ctx, getInput(o.SrcBucket, key, e.Version))
	if err != nil {
		return fmt.Errorf("read source %q failed", key)
	}
	defer func() { _ = out.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(out.Body, maxObjectBytes+1))
	if err != nil || int64(len(body)) != e.Size {
		return fmt.Errorf("read source %q returned an unexpected length", key)
	}
	meta := map[string]string{sourceVersionMeta: e.Version}
	for k, v := range out.Metadata {
		meta[k] = v
	}
	_, err = o.Dst.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(o.DstBucket), Key: aws.String(key), Body: bytes.NewReader(body), ContentLength: aws.Int64(int64(len(body))),
		ContentType: out.ContentType, CacheControl: out.CacheControl, ContentDisposition: out.ContentDisposition, ContentEncoding: out.ContentEncoding, ContentLanguage: out.ContentLanguage, Metadata: meta})
	if err != nil {
		return fmt.Errorf("write target %q failed", key)
	}
	return nil
}

func verify(ctx context.Context, o Options, src, dst map[string][]entry, report *Report) error {
	for _, es := range src {
		report.SrcEntries += len(es)
		report.SrcBytes += total(es)
	}
	for _, es := range dst {
		report.DstEntries += len(es)
		report.DstBytes += total(es)
	}
	if report.SrcEntries != report.DstEntries {
		report.Mismatches = append(report.Mismatches, Mismatch{KindCount, "", fmt.Sprintf("source %d, target %d", report.SrcEntries, report.DstEntries)})
	}
	if report.SrcBytes != report.DstBytes {
		report.Mismatches = append(report.Mismatches, Mismatch{KindBytes, "", fmt.Sprintf("source %d, target %d", report.SrcBytes, report.DstBytes)})
	}
	keys := map[string]bool{}
	for k := range src {
		keys[k] = true
	}
	for k := range dst {
		keys[k] = true
	}
	type pair struct {
		key      string
		src, dst entry
	}
	var versions []pair
	for _, key := range sortedKeys(keys) {
		s, d := src[key], dst[key]
		if !equalShape(s, d) {
			report.Mismatches = append(report.Mismatches, Mismatch{KindVersions, key, fmt.Sprintf("version history differs (source %d entries, target %d)", len(s), len(d))})
			continue
		}
		for i := range s {
			if !s[i].Marker {
				versions = append(versions, pair{key, s[i], d[i]})
			}
		}
	}
	step := 1
	if o.SampleSize > 0 && len(versions) > o.SampleSize {
		step = (len(versions) + o.SampleSize - 1) / o.SampleSize
	}
	for i := 0; i < len(versions); i += step {
		p := versions[i]
		want, err := digest(ctx, o.Src, o.SrcBucket, p.key, p.src.Version)
		if err != nil {
			return err
		}
		got, err := digest(ctx, o.Dst, o.DstBucket, p.key, p.dst.Version)
		if err != nil {
			return err
		}
		report.ChecksumsVerified++
		if want != got {
			report.Mismatches = append(report.Mismatches, Mismatch{KindChecksum, p.key, "sha256 of version " + p.src.Version + " differs"})
		}
	}
	return nil
}

func digest(ctx context.Context, r Reader, bucket, key, version string) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	out, err := r.GetObject(ctx, getInput(bucket, key, version))
	if err != nil {
		return sum, fmt.Errorf("read %q for checksum failed", key)
	}
	defer func() { _ = out.Body.Close() }()
	h := sha256.New()
	if _, err = io.Copy(h, out.Body); err != nil {
		return sum, fmt.Errorf("read %q for checksum failed", key)
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

func getInput(bucket, key, version string) *s3.GetObjectInput {
	in := &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
	if version != "" && version != "null" {
		in.VersionId = aws.String(version)
	}
	return in
}

func total(es []entry) (n int64) {
	for _, e := range es {
		n += e.Size
	}
	return n
}

// samePrefix reports whether the target entries match the leading source entries in type and size.
func samePrefix(src, dst []entry) bool {
	return equalShape(src[:len(dst)], dst)
}

func equalShape(a, b []entry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Marker != b[i].Marker || a[i].Size != b[i].Size {
			return false
		}
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// list returns every version and delete marker per key, oldest first. Ties in the
// one-second S3 timestamps put the latest entry last.
func list(ctx context.Context, r Reader, bucket string) (map[string][]entry, error) {
	out := map[string][]entry{}
	in := &s3.ListObjectVersionsInput{Bucket: aws.String(bucket)}
	for {
		page, err := r.ListObjectVersions(ctx, in)
		if err != nil {
			return nil, errors.New("object version listing failed")
		}
		for _, v := range page.Versions {
			key := aws.ToString(v.Key)
			out[key] = append(out[key], entry{Version: aws.ToString(v.VersionId), Size: aws.ToInt64(v.Size), Modified: aws.ToTime(v.LastModified), Latest: aws.ToBool(v.IsLatest)})
		}
		for _, m := range page.DeleteMarkers {
			key := aws.ToString(m.Key)
			out[key] = append(out[key], entry{Marker: true, Version: aws.ToString(m.VersionId), Modified: aws.ToTime(m.LastModified), Latest: aws.ToBool(m.IsLatest)})
		}
		if !aws.ToBool(page.IsTruncated) {
			break
		}
		in.KeyMarker, in.VersionIdMarker = page.NextKeyMarker, page.NextVersionIdMarker
	}
	for _, es := range out {
		sort.SliceStable(es, func(i, j int) bool {
			if !es[i].Modified.Equal(es[j].Modified) {
				return es[i].Modified.Before(es[j].Modified)
			}
			return !es[i].Latest && es[j].Latest
		})
	}
	return out, nil
}
