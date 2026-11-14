package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const maxObjectBytes = 256 << 20

// PutVersioned overwrites a key as a new version and refuses unversioned buckets.
func (s *S3) PutVersioned(ctx context.Context, key string, body []byte, meta map[string]string) (string, error) {
	out, err := s.Client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key), Body: bytes.NewReader(body), ContentLength: aws.Int64(int64(len(body))), Metadata: meta})
	if err != nil {
		return "", errors.New("object put failed")
	}
	id := aws.ToString(out.VersionId)
	if id == "" || id == "null" {
		return "", errors.New("object versioning is not enabled")
	}
	return id, nil
}

// GetCurrent reads the newest version; a delete marker reads as an error.
func (s *S3) GetCurrent(ctx context.Context, key string) ([]byte, error) {
	return s.read(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
}

// GetVersion reads one stored version.
func (s *S3) GetVersion(ctx context.Context, key, versionID string) ([]byte, error) {
	return s.read(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key), VersionId: aws.String(versionID)})
}

func (s *S3) read(ctx context.Context, in *s3.GetObjectInput) ([]byte, error) {
	out, err := s.Client.GetObject(ctx, in)
	if err != nil {
		return nil, errors.New("object get failed")
	}
	defer func() { _ = out.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(out.Body, maxObjectBytes+1))
	if err != nil || len(data) > maxObjectBytes {
		return nil, errors.New("object read failed")
	}
	return data, nil
}

// ListVersions returns versions of exactly one key, newest first, with metadata.
func (s *S3) ListVersions(ctx context.Context, key string) ([]Version, error) {
	var out []Version
	var markers []Version
	pages := s3.NewListObjectVersionsPaginator(s.Client, &s3.ListObjectVersionsInput{Bucket: aws.String(s.Bucket), Prefix: aws.String(key)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, errors.New("object version listing failed")
		}
		for _, v := range page.Versions {
			if aws.ToString(v.Key) != key {
				continue
			}
			head, err := s.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key), VersionId: v.VersionId})
			if err != nil {
				return nil, errors.New("object version metadata failed")
			}
			out = append(out, Version{ID: aws.ToString(v.VersionId), Latest: aws.ToBool(v.IsLatest), Size: aws.ToInt64(v.Size), Modified: aws.ToTime(v.LastModified), Metadata: head.Metadata})
		}
		for _, m := range page.DeleteMarkers {
			if aws.ToString(m.Key) == key {
				markers = append(markers, Version{ID: aws.ToString(m.VersionId), Latest: aws.ToBool(m.IsLatest), DeleteMarker: true, Modified: aws.ToTime(m.LastModified)})
			}
		}
	}
	out = append(out, markers...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}
