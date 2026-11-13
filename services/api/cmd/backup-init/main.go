// Command backup-init creates the local-only RustFS backup writer.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/backupadmin"
)

func main() {
	if os.Getenv("DUOLIFY_ENV") != "development" {
		log.Fatal("backup-init is development-only")
	}
	if os.Getenv("BACKUP_S3_ACCESS_KEY_ID") == os.Getenv("S3_ACCESS_KEY_ID") {
		log.Fatal("application and backup storage identities must differ")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	root := backup.S3Options{Endpoint: os.Getenv("BACKUP_S3_ENDPOINT"), Region: os.Getenv("BACKUP_S3_REGION"), Bucket: os.Getenv("BACKUP_S3_BUCKET"), ApplicationBucket: os.Getenv("S3_BUCKET"), AccessKey: os.Getenv("S3_ROOT_ACCESS_KEY_ID"), SecretKey: os.Getenv("S3_ROOT_SECRET_ACCESS_KEY")}
	err := backupadmin.Provision(ctx, root, os.Getenv("BACKUP_S3_ACCESS_KEY_ID"), os.Getenv("BACKUP_S3_SECRET_ACCESS_KEY"))
	if err != nil {
		log.Fatal("local backup writer initialization failed")
	}
	if err := backupadmin.ProvisionApplication(ctx, root, os.Getenv("S3_ACCESS_KEY_ID"), os.Getenv("S3_SECRET_ACCESS_KEY")); err != nil {
		log.Fatal("local application storage initialization failed")
	}
	for _, identity := range []struct {
		env, name string
		provision func(context.Context, backup.S3Options, string, string) error
	}{
		{"BACKUP_EXPIRE_S3", "retention", backupadmin.ProvisionRetention},
		{"BACKUP_RESTORE_S3", "restorer", backupadmin.ProvisionRestorer},
		{"ASSET_RECOVERY_S3", "asset recovery", backupadmin.ProvisionAssetRecovery},
	} {
		if err := identity.provision(ctx, root, os.Getenv(identity.env+"_ACCESS_KEY_ID"), os.Getenv(identity.env+"_SECRET_ACCESS_KEY")); err != nil {
			log.Fatalf("local %s identity initialization failed", identity.name)
		}
	}
}
