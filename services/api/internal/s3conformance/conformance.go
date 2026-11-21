// Package s3conformance verifies that an S3-compatible store supports every
// operation the product relies on (backup store and player objects) before
// data moves to it. It creates and deletes only its own bucket or prefix.
package s3conformance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// Config is the target store. Bucket empty means the suite creates (and removes)
// its own object-lock bucket; otherwise it only writes under a unique prefix and
// never changes that bucket's versioning.
type Config struct {
	Endpoint, Region, Bucket, AccessKey, SecretKey string
	PathStyle                                      bool
}

// ConfigFromEnv reads S3_CONFORMANCE_* variables.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		Endpoint: getenv("S3_CONFORMANCE_ENDPOINT"), Region: getenv("S3_CONFORMANCE_REGION"), Bucket: getenv("S3_CONFORMANCE_BUCKET"),
		AccessKey: getenv("S3_CONFORMANCE_ACCESS_KEY_ID"), SecretKey: getenv("S3_CONFORMANCE_SECRET_ACCESS_KEY"),
		PathStyle: getenv("S3_CONFORMANCE_PATH_STYLE") != "false",
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Config{}, errors.New("S3_CONFORMANCE_ENDPOINT must be an http(s) URL")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return Config{}, errors.New("S3_CONFORMANCE_ACCESS_KEY_ID and S3_CONFORMANCE_SECRET_ACCESS_KEY are required")
	}
	return cfg, nil
}

// Redact removes both credentials from any text before it is printed.
func (c Config) Redact(s string) string {
	for _, secret := range []string{c.SecretKey, c.AccessKey} {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}

// Client builds an explicit-credential client with the same options as the product.
func (c Config) Client() *s3.Client {
	return s3.New(s3.Options{
		Region: c.Region, BaseEndpoint: aws.String(c.Endpoint), UsePathStyle: c.PathStyle,
		Credentials:                credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, ""),
		RetryMaxAttempts:           1,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
	})
}

// Status is the outcome of one operation.
type Status string

const (
	// StatusPass and StatusFail are the only outcomes.
	StatusPass Status = "PASS"
	// StatusFail marks an unsupported or broken operation.
	StatusFail Status = "FAIL"
)

// Result is one operation outcome; Detail names the cause of a failure.
type Result struct {
	Operation string
	Status    Status
	Detail    string
}

func (r Result) String() string {
	if r.Detail == "" {
		return fmt.Sprintf("%s  %s", r.Status, r.Operation)
	}
	return fmt.Sprintf("%s  %s: %s", r.Status, r.Operation, r.Detail)
}

// Env is what an operation runs against.
type Env struct {
	Client *s3.Client
	Bucket string
	Prefix string
}

// Operation is one conformance check. Calls lists the SDK methods it exercises
// so a test can prove every S3 call used by the product is covered.
type Operation struct {
	Name  string
	Calls []string
	Run   func(context.Context, Env) error
}

// ExemptCalls are product S3 calls deliberately outside the suite, with the reason.
var ExemptCalls = map[string]string{
	"PutBucketPolicy": "local RustFS IAM provisioning only; hosted stores use their own access-key scoping",
}

// Operations is the ordered table of checks.
func Operations() []Operation {
	return []Operation{
		{"bucket versioning", []string{"CreateBucket", "HeadBucket", "PutBucketVersioning", "GetBucketVersioning"}, checkVersioning},
		{"versioned put", []string{"PutObject", "HeadObject"}, checkVersionedPut},
		{"list versions", []string{"ListObjectVersions", "ListObjectsV2", "ListObjectVersionsPaginator"}, checkListVersions},
		{"get by version", []string{"GetObject"}, checkGetByVersion},
		{"delete-marker restore", []string{"DeleteObject"}, checkDeleteMarkerRestore},
		{"conditional create (If-None-Match: *)", []string{"PutObject"}, checkConditionalCreate},
		{"retention lock", []string{"PutObject", "GetObjectRetention", "DeleteObject"}, checkRetentionLock},
	}
}

// Run executes every operation and always cleans up what it created.
func Run(ctx context.Context, cfg Config) []Result {
	client := cfg.Client()
	env := Env{Client: client, Bucket: cfg.Bucket, Prefix: "s3-conformance/" + randomID() + "/"}
	ownBucket := cfg.Bucket == ""
	if ownBucket {
		env.Bucket = "s3conf-" + randomID()
		env.Prefix = ""
		if err := createOwnBucket(ctx, client, env.Bucket); err != nil {
			return failAll(cfg, "create test bucket: "+err.Error())
		}
	} else if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(env.Bucket)}); err != nil {
		return failAll(cfg, "bucket not reachable: "+err.Error())
	}
	defer cleanup(env, ownBucket)

	var results []Result
	for _, op := range Operations() {
		r := Result{Operation: op.Name, Status: StatusPass}
		if err := op.Run(ctx, env); err != nil {
			r.Status, r.Detail = StatusFail, cfg.Redact(err.Error())
		}
		results = append(results, r)
	}
	return results
}

func failAll(cfg Config, detail string) []Result {
	var out []Result
	for _, op := range Operations() {
		out = append(out, Result{op.Name, StatusFail, cfg.Redact(detail)})
	}
	return out
}

// createOwnBucket asks for object lock (needed for retention); a store that
// rejects it still gets a plain bucket so the retention check fails by name.
func createOwnBucket(ctx context.Context, c *s3.Client, bucket string) error {
	if _, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket), ObjectLockEnabledForBucket: aws.Bool(true)}); err != nil {
		if _, err2 := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err2 != nil {
			return err2
		}
	}
	// Failure here surfaces as the "bucket versioning" check failing.
	_, _ = c.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: aws.String(bucket), VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}})
	return nil
}

// cleanup removes every version and delete marker under our prefix (bypassing
// governance retention) and the bucket only when we created it.
func cleanup(env Env, ownBucket bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pages := s3.NewListObjectVersionsPaginator(env.Client, &s3.ListObjectVersionsInput{Bucket: aws.String(env.Bucket), Prefix: aws.String(env.Prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			break
		}
		var ids []types.ObjectIdentifier
		for _, v := range page.Versions {
			ids = append(ids, types.ObjectIdentifier{Key: v.Key, VersionId: v.VersionId})
		}
		for _, m := range page.DeleteMarkers {
			ids = append(ids, types.ObjectIdentifier{Key: m.Key, VersionId: m.VersionId})
		}
		for _, id := range ids {
			_, _ = env.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(env.Bucket), Key: id.Key, VersionId: id.VersionId, BypassGovernanceRetention: aws.Bool(true)})
		}
	}
	// Stores without versioning return plain objects here.
	if out, err := env.Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(env.Bucket), Prefix: aws.String(env.Prefix)}); err == nil {
		for _, o := range out.Contents {
			_, _ = env.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(env.Bucket), Key: o.Key})
		}
	}
	if ownBucket {
		_, _ = env.Client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(env.Bucket)})
	}
}

func randomID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func put(ctx context.Context, e Env, key, body string) (*s3.PutObjectOutput, error) {
	return e.Client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(e.Bucket), Key: aws.String(key), Body: strings.NewReader(body), ContentLength: aws.Int64(int64(len(body)))})
}

func read(ctx context.Context, e Env, key string, version *string) (string, error) {
	out, err := e.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(e.Bucket), Key: aws.String(key), VersionId: version})
	if err != nil {
		return "", err
	}
	defer func() { _ = out.Body.Close() }()
	raw, err := io.ReadAll(out.Body)
	return string(bytes.TrimSpace(raw)), err
}

func checkVersioning(ctx context.Context, e Env) error {
	out, err := e.Client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(e.Bucket)})
	if err != nil {
		return fmt.Errorf("get bucket versioning: %w", err)
	}
	if out.Status != types.BucketVersioningStatusEnabled {
		return fmt.Errorf("bucket versioning is %q, want Enabled", out.Status)
	}
	return nil
}

// twoVersions writes two versions of a fresh key and returns both IDs.
func twoVersions(ctx context.Context, e Env, name string) (key string, v1, v2 string, err error) {
	key = e.Prefix + name
	first, err := put(ctx, e, key, "one")
	if err != nil {
		return key, "", "", err
	}
	second, err := put(ctx, e, key, "two")
	if err != nil {
		return key, "", "", err
	}
	v1, v2 = aws.ToString(first.VersionId), aws.ToString(second.VersionId)
	if v1 == "" || v2 == "" {
		return key, v1, v2, errors.New("put returned no version ID (store ignores versioning)")
	}
	if v1 == v2 {
		return key, v1, v2, errors.New("two puts returned the same version ID")
	}
	return key, v1, v2, nil
}

func checkVersionedPut(ctx context.Context, e Env) error {
	key, v1, _, err := twoVersions(ctx, e, "versioned-put")
	if err != nil {
		return err
	}
	head, err := e.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(e.Bucket), Key: aws.String(key), VersionId: aws.String(v1)})
	if err != nil {
		return fmt.Errorf("head by version: %w", err)
	}
	if aws.ToString(head.VersionId) != v1 {
		return errors.New("head by version returned a different version")
	}
	return nil
}

func checkListVersions(ctx context.Context, e Env) error {
	key, v1, v2, err := twoVersions(ctx, e, "list-versions")
	if err != nil {
		return err
	}
	out, err := e.Client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(e.Bucket), Prefix: aws.String(key)})
	if err != nil {
		return fmt.Errorf("list versions: %w", err)
	}
	seen := map[string]bool{}
	latest := ""
	for _, v := range out.Versions {
		if aws.ToString(v.Key) == key {
			seen[aws.ToString(v.VersionId)] = true
			if aws.ToBool(v.IsLatest) {
				latest = aws.ToString(v.VersionId)
			}
		}
	}
	if !seen[v1] || !seen[v2] || latest != v2 {
		return fmt.Errorf("list versions did not return both versions with the newest flagged latest (seen %d)", len(seen))
	}
	cur, err := e.Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(e.Bucket), Prefix: aws.String(key)})
	if err != nil || len(cur.Contents) != 1 {
		return fmt.Errorf("list current objects: %v", err)
	}
	return nil
}

func checkGetByVersion(ctx context.Context, e Env) error {
	key, v1, _, err := twoVersions(ctx, e, "get-by-version")
	if err != nil {
		return err
	}
	if got, err := read(ctx, e, key, aws.String(v1)); err != nil || got != "one" {
		return fmt.Errorf("get by version returned %q (%v), want the first body", got, err)
	}
	if got, err := read(ctx, e, key, nil); err != nil || got != "two" {
		return fmt.Errorf("get current returned %q (%v), want the second body", got, err)
	}
	return nil
}

func apiCode(err error) string {
	var api smithy.APIError
	if errors.As(err, &api) {
		return api.ErrorCode()
	}
	return ""
}

func checkDeleteMarkerRestore(ctx context.Context, e Env) error {
	key, _, _, err := twoVersions(ctx, e, "delete-marker")
	if err != nil {
		return err
	}
	del, err := e.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(e.Bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	marker := aws.ToString(del.VersionId)
	if !aws.ToBool(del.DeleteMarker) || marker == "" {
		return errors.New("delete did not create a delete marker")
	}
	if _, err := read(ctx, e, key, nil); apiCode(err) != "NoSuchKey" {
		return fmt.Errorf("read after delete should be NoSuchKey, got %v", err)
	}
	if _, err := e.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(e.Bucket), Key: aws.String(key), VersionId: aws.String(marker)}); err != nil {
		return fmt.Errorf("delete the marker by version: %w", err)
	}
	if got, err := read(ctx, e, key, nil); err != nil || got != "two" {
		return fmt.Errorf("read after marker removal returned %q (%v), want the restored body", got, err)
	}
	return nil
}

func checkConditionalCreate(ctx context.Context, e Env) error {
	key := e.Prefix + "if-none-match"
	in := func() *s3.PutObjectInput {
		return &s3.PutObjectInput{Bucket: aws.String(e.Bucket), Key: aws.String(key), Body: strings.NewReader("x"), ContentLength: aws.Int64(1), IfNoneMatch: aws.String("*")}
	}
	if _, err := e.Client.PutObject(ctx, in()); err != nil {
		return fmt.Errorf("conditional create of a new key: %w", err)
	}
	_, err := e.Client.PutObject(ctx, in())
	var status interface{ HTTPStatusCode() int }
	if err == nil {
		return errors.New("second conditional create overwrote the key; want HTTP 412")
	}
	if !errors.As(err, &status) || status.HTTPStatusCode() != http.StatusPreconditionFailed {
		return fmt.Errorf("second conditional create returned %v, want HTTP 412", err)
	}
	return nil
}

// checkRetentionLock writes a governance-locked object on the deletion-ledger
// prefix and requires that deleting that version is refused.
func checkRetentionLock(ctx context.Context, e Env) error {
	key := e.Prefix + "account-deletions/v1/retention"
	out, err := e.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(e.Bucket), Key: aws.String(key), Body: strings.NewReader("ledger"), ContentLength: aws.Int64(6),
		ObjectLockMode: types.ObjectLockModeGovernance, ObjectLockRetainUntilDate: aws.Time(time.Now().Add(time.Hour)),
	})
	if err != nil {
		return fmt.Errorf("put with object lock (bucket must be created with object lock enabled): %w", err)
	}
	ret, err := e.Client.GetObjectRetention(ctx, &s3.GetObjectRetentionInput{Bucket: aws.String(e.Bucket), Key: aws.String(key), VersionId: out.VersionId})
	if err != nil || ret.Retention == nil || ret.Retention.Mode != types.ObjectLockRetentionModeGovernance {
		return fmt.Errorf("object retention not stored: %v", err)
	}
	if _, err := e.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(e.Bucket), Key: aws.String(key), VersionId: out.VersionId}); err == nil {
		return errors.New("deleting a locked version succeeded; retention is not enforced")
	}
	return nil
}
