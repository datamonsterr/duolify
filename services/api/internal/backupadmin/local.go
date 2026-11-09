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

// ProvisionApplication confines the public API to the application bucket. Root
// credentials belong only to local initialization and retention administration.
func ProvisionApplication(ctx context.Context, root backup.S3Options, key, secret string) error {
	if root.ApplicationBucket == "" || root.ApplicationBucket == root.Bucket || key == "" || secret == "" || key == root.AccessKey {
		return errors.New("separate application storage identity required")
	}
	endpoint, err := url.Parse(root.Endpoint)
	if err != nil {
		return err
	}
	admin, err := madmin.NewWithOptions(endpoint.Host, &madmin.Options{Creds: miniocredentials.NewStaticV4(root.AccessKey, root.SecretKey, ""), Secure: endpoint.Scheme == "https"})
	if err != nil {
		return err
	}
	policy, err := json.Marshal(map[string]any{"Version": "2012-10-17", "Statement": []any{
		map[string]any{"Effect": "Allow", "Action": []string{"s3:*"}, "Resource": []string{"arn:aws:s3:::" + root.ApplicationBucket, "arn:aws:s3:::" + root.ApplicationBucket + "/*"}},
	}})
	if err != nil {
		return err
	}
	if err := admin.AddUser(ctx, key, secret); err != nil {
		return err
	}
	if err := admin.AddCannedPolicy(ctx, "duolify-application-storage", policy); err != nil {
		return err
	}
	_, err = admin.AttachPolicy(ctx, madmin.PolicyAssociationReq{Policies: []string{"duolify-application-storage"}, User: key})
	return err
}
