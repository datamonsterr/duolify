// Package backupadmin provisions scoped local RustFS credentials. It is only
// linked by the development initializer, never by the public API or worker.
package backupadmin

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	madmin "github.com/minio/madmin-go/v3"
	miniocredentials "github.com/minio/minio-go/v7/pkg/credentials"
)

// Provision creates a dedicated writer with no delete or administration rights.
// A recovery/retention administrator is deliberately separate from this user.
func Provision(ctx context.Context, root backup.S3Options, writerKey, writerSecret string) error {
	if root.AccessKey == writerKey || writerKey == "" || writerSecret == "" {
		return errors.New("distinct local writer credentials required")
	}
	endpoint, err := url.Parse(root.Endpoint)
	if err != nil {
		return errors.New("invalid local storage endpoint")
	}
	admin, err := madmin.NewWithOptions(endpoint.Host, &madmin.Options{Creds: miniocredentials.NewStaticV4(root.AccessKey, root.SecretKey, ""), Secure: endpoint.Scheme == "https"})
	if err != nil {
		return err
	}
	store, err := backup.NewS3(root)
	if err != nil {
		return err
	}
	if _, err := store.Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(root.Bucket)}); err != nil {
		if _, err := store.Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(root.Bucket)}); err != nil {
			return err
		}
	}
	policy := map[string]any{"Version": "2012-10-17", "Statement": []any{
		map[string]any{"Effect": "Allow", "Action": []string{"s3:ListBucket"}, "Resource": []string{"arn:aws:s3:::" + root.Bucket}},
		map[string]any{"Effect": "Allow", "Action": []string{"s3:PutObject", "s3:GetObject"}, "Resource": []string{"arn:aws:s3:::" + root.Bucket + "/*"}},
		map[string]any{"Effect": "Deny", "Action": []string{"s3:DeleteObject", "s3:DeleteObjectVersion", "s3:DeleteBucket", "s3:PutBucketPolicy"}, "Resource": []string{"arn:aws:s3:::" + root.Bucket, "arn:aws:s3:::" + root.Bucket + "/*"}},
	}}
	body, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	if err := admin.AddUser(ctx, writerKey, writerSecret); err != nil {
		return err
	}
	if err := admin.AddCannedPolicy(ctx, "duolify-backup-writer", body); err != nil {
		return err
	}
	_, err = admin.AttachPolicy(ctx, madmin.PolicyAssociationReq{Policies: []string{"duolify-backup-writer"}, User: writerKey})
	return err
}

// ProvisionApplication confines the public API to the versioned application
// bucket: read, write and delete-marker only. It cannot delete versions, change
// versioning or policy, or touch the backup bucket. Root credentials belong only
// to local initialization and retention administration.
func ProvisionApplication(ctx context.Context, root backup.S3Options, key, secret string) error {
	if root.ApplicationBucket == "" || root.ApplicationBucket == root.Bucket || key == "" || secret == "" || key == root.AccessKey {
		return errors.New("separate application storage identity required")
	}
	store, err := backup.NewS3(root)
	if err != nil {
		return err
	}
	if _, err := store.Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(root.ApplicationBucket)}); err != nil {
		if _, err := store.Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(root.ApplicationBucket)}); err != nil {
			return err
		}
	}
	if _, err := store.Client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: aws.String(root.ApplicationBucket), VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}}); err != nil {
		return err
	}
	app, objects := arn(root.ApplicationBucket), arn(root.ApplicationBucket+"/*")
	return attach(ctx, root, "duolify-application-storage", key, secret, []any{
		allow([]string{"s3:ListBucket"}, app),
		allow([]string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject"}, objects),
		deny([]string{"s3:DeleteObjectVersion", "s3:PutBucketVersioning", "s3:DeleteBucket", "s3:PutBucketPolicy", "s3:PutBucketLifecycle"}, app, objects),
	})
}

// ProvisionRetention creates the separate expiry identity: it lists the backup
// bucket and deletes dumps only. It cannot read, write or delete the
// account-deletions ledger.
func ProvisionRetention(ctx context.Context, root backup.S3Options, key, secret string) error {
	if err := distinct(root, key, secret); err != nil {
		return err
	}
	b := root.Bucket
	return attach(ctx, root, "duolify-backup-expirer", key, secret, []any{
		allow([]string{"s3:ListBucket"}, arn(b)),
		allow([]string{"s3:DeleteObject"}, arn(b+"/20*"), arn(b+"/financial/*")),
		deny([]string{"s3:DeleteObject"}, arn(b+"/account-deletions/*")),
		deny([]string{"s3:GetObject", "s3:PutObject", "s3:DeleteObjectVersion", "s3:DeleteBucket", "s3:PutBucketPolicy"}, arn(b), arn(b+"/*")),
	})
}

// ProvisionRestorer creates a read-only backup identity for restore drills.
func ProvisionRestorer(ctx context.Context, root backup.S3Options, key, secret string) error {
	if err := distinct(root, key, secret); err != nil {
		return err
	}
	b := root.Bucket
	return attach(ctx, root, "duolify-backup-restorer", key, secret, []any{
		allow([]string{"s3:ListBucket"}, arn(b)),
		allow([]string{"s3:GetObject"}, arn(b+"/*")),
		deny([]string{"s3:PutObject", "s3:DeleteObject", "s3:DeleteObjectVersion", "s3:DeleteBucket", "s3:PutBucketPolicy"}, arn(b), arn(b+"/*")),
	})
}

// ProvisionAssetRecovery creates the operator identity that reads object
// versions and appends tracked recovery versions to the application bucket. It
// cannot delete objects or versions or alter versioning.
func ProvisionAssetRecovery(ctx context.Context, root backup.S3Options, key, secret string) error {
	if err := distinct(root, key, secret); err != nil {
		return err
	}
	a := root.ApplicationBucket
	return attach(ctx, root, "duolify-asset-recovery", key, secret, []any{
		allow([]string{"s3:ListBucket", "s3:ListBucketVersions"}, arn(a)),
		allow([]string{"s3:GetObject", "s3:GetObjectVersion", "s3:PutObject"}, arn(a+"/*")),
		deny([]string{"s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutBucketVersioning", "s3:DeleteBucket", "s3:PutBucketPolicy"}, arn(a), arn(a+"/*")),
	})
}

func distinct(root backup.S3Options, key, secret string) error {
	if root.AccessKey == key || key == "" || secret == "" {
		return errors.New("distinct local credentials required")
	}
	return nil
}

func arn(resource string) string { return "arn:aws:s3:::" + resource }

func allow(actions []string, resources ...string) map[string]any {
	return map[string]any{"Effect": "Allow", "Action": actions, "Resource": resources}
}

func deny(actions []string, resources ...string) map[string]any {
	return map[string]any{"Effect": "Deny", "Action": actions, "Resource": resources}
}

func attach(ctx context.Context, root backup.S3Options, name, key, secret string, statements []any) error {
	endpoint, err := url.Parse(root.Endpoint)
	if err != nil {
		return errors.New("invalid local storage endpoint")
	}
	admin, err := madmin.NewWithOptions(endpoint.Host, &madmin.Options{Creds: miniocredentials.NewStaticV4(root.AccessKey, root.SecretKey, ""), Secure: endpoint.Scheme == "https"})
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"Version": "2012-10-17", "Statement": statements})
	if err != nil {
		return err
	}
	if err := admin.AddUser(ctx, key, secret); err != nil {
		return err
	}
	if err := admin.AddCannedPolicy(ctx, name, body); err != nil {
		return err
	}
	_, err = admin.AttachPolicy(ctx, madmin.PolicyAssociationReq{Policies: []string{name}, User: key})
	return err
}
