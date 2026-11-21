package backup

import "testing"

func TestBackupReadFallbackConfig(t *testing.T) {
	base := map[string]string{"BACKUP_S3_ENDPOINT": "https://new.example.test", "BACKUP_S3_BUCKET": "new", "BACKUP_S3_REGION": "r", "BACKUP_S3_ACCESS_KEY_ID": "k", "BACKUP_S3_SECRET_ACCESS_KEY": "s"}
	get := func(extra map[string]string) func(string) string {
		return func(k string) string {
			if v, ok := extra[k]; ok {
				return v
			}
			return base[k]
		}
	}
	cfg, err := Load(get(nil))
	if err != nil || cfg.S3.Fallback != nil {
		t.Fatalf("fallback must be off by default: %v %v", cfg.S3.Fallback, err)
	}
	if _, err = Load(get(map[string]string{"BACKUP_S3_FALLBACK_ENABLED": "true"})); err == nil {
		t.Fatal("fallback on without an old store accepted")
	}
	old := map[string]string{"BACKUP_S3_FALLBACK_ENABLED": "true", "BACKUP_OLD_S3_ENDPOINT": "https://old.example.test", "BACKUP_OLD_S3_REGION": "r", "BACKUP_OLD_S3_BUCKET": "old", "BACKUP_OLD_S3_ACCESS_KEY_ID": "k2", "BACKUP_OLD_S3_SECRET_ACCESS_KEY": "s2"}
	if cfg, err = Load(get(old)); err != nil || cfg.S3.Fallback == nil || cfg.S3.Fallback.Bucket != "old" {
		t.Fatalf("complete config rejected: %v", err)
	}
	old["BACKUP_OLD_S3_ENDPOINT"], old["BACKUP_OLD_S3_BUCKET"] = "https://new.example.test", "new"
	if _, err = Load(get(old)); err == nil {
		t.Fatal("old store equal to the new store accepted")
	}
}
