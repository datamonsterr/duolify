package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProtectedCharacterAsset records version/key/checksum, never a private prompt or screening record.
type ProtectedCharacterAsset struct {
	Key       string `json:"key"`
	Version   string `json:"version_id"`
	SHA256    string `json:"sha256"`
	BackupKey string `json:"backup_key"`
}

// CharacterAssetProtection snapshots all retained rig and narration objects after pg_dump finishes.
// The 30-day purge grace exceeds dump retention, so a concurrent reference ending cannot lose bytes.
type CharacterAssetProtection struct {
	Pool        *pgxpool.Pool
	Application *S3
	Protected   Objects
	Recipient   string
}

// Protect writes encrypted protected copies and a key/version/checksum manifest for one nightly dump.
func (p CharacterAssetProtection) Protect(ctx context.Context, date string) error {
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return err
	}
	if p.Application == nil || p.Protected == nil {
		return errors.New("asset protection unavailable")
	}
	recipient, err := age.ParseX25519Recipient(p.Recipient)
	if err != nil {
		return err
	}
	rows, err := p.Pool.Query(ctx, `SELECT object_key,content_hash FROM shared_character_assets WHERE purged_at IS NULL UNION SELECT audio_key,'' FROM narration_assets WHERE ref_count>0 UNION SELECT alignment_key,'' FROM narration_assets WHERE ref_count>0 UNION SELECT manifest_key,'' FROM lesson_manifests WHERE starts_with(manifest_key,'lessons/generated/') ORDER BY 1`)
	if err != nil {
		return err
	}
	type ref struct{ key, checksum string }
	var refs []ref
	for rows.Next() {
		var r ref
		if err = rows.Scan(&r.key, &r.checksum); err != nil {
			rows.Close()
			return err
		}
		if r.key != "" {
			refs = append(refs, r)
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return rows.Err()
	}
	var manifest []ProtectedCharacterAsset
	putEncrypted := func(key string, raw []byte) error {
		var b bytes.Buffer
		w, e := age.Encrypt(&b, recipient)
		if e != nil {
			return e
		}
		if _, e = w.Write(raw); e != nil {
			return e
		}
		if e = w.Close(); e != nil {
			return e
		}
		return p.Protected.Put(ctx, key, bytes.NewReader(b.Bytes()), int64(b.Len()))
	}
	for i := 0; i < len(refs); i++ {
		r := refs[i]
		object, e := p.Application.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(p.Application.Bucket), Key: aws.String(r.key)})
		if e != nil {
			return e
		}
		raw, e := io.ReadAll(io.LimitReader(object.Body, maxObjectBytes+1))
		_ = object.Body.Close()
		if e != nil || len(raw) > maxObjectBytes || aws.ToString(object.VersionId) == "" || aws.ToString(object.VersionId) == "null" {
			return errors.New("asset must be bounded and versioned")
		}
		sum := sha256.Sum256(raw)
		hash := hex.EncodeToString(sum[:])
		if r.checksum != "" && r.checksum != hash {
			return errors.New("asset checksum mismatch")
		}
		if strings.HasSuffix(r.key, "/manifest.json") && strings.HasPrefix(r.key, "lessons/generated/") {
			var manifest struct {
				Slides []struct {
					Key string `json:"html_key"`
					SHA string `json:"html_sha256"`
				} `json:"slides"`
			}
			if json.Unmarshal(raw, &manifest) != nil {
				return errors.New("invalid generated manifest")
			}
			for _, slide := range manifest.Slides {
				if !strings.HasPrefix(slide.Key, "lessons/generated/") || !blockReference.MatchString(slide.SHA) {
					return errors.New("invalid generated asset reference")
				}
				refs = append(refs, ref{key: slide.Key, checksum: slide.SHA})
			}
		}
		key := date + "/character-assets/" + hash + ".age"
		if e = putEncrypted(key, raw); e != nil {
			return e
		}
		manifest = append(manifest, ProtectedCharacterAsset{Key: r.key, Version: aws.ToString(object.VersionId), SHA256: hash, BackupKey: key})
	}
	raw, _ := json.Marshal(manifest)
	return putEncrypted(CharacterAssetManifestKey(date), raw)
}

// CharacterAssetManifestKey is the protected manifest for a backup day.
func CharacterAssetManifestKey(date string) string {
	return date + "/character-assets/manifest.json.age"
}

// RestoreCharacterAssets restores protected bytes with key/version/checksum verification and no LLM/TTS.
// Deletion and moderation replay remains mandatory before serving the restored database.
func RestoreCharacterAssets(ctx context.Context, date string, store BlockStore, identity age.Identity, put func(context.Context, string, []byte) error) error {
	decrypt := func(key string) ([]byte, error) {
		raw, e := store.GetCurrent(ctx, key)
		if e != nil {
			return nil, e
		}
		r, e := age.Decrypt(bytes.NewReader(raw), identity)
		if e != nil {
			return nil, e
		}
		return io.ReadAll(io.LimitReader(r, maxObjectBytes+1))
	}
	raw, err := decrypt(CharacterAssetManifestKey(date))
	if err != nil {
		return err
	}
	var manifest []ProtectedCharacterAsset
	if json.Unmarshal(raw, &manifest) != nil {
		return errors.New("invalid asset manifest")
	}
	for _, entry := range manifest {
		if !strings.HasPrefix(entry.Key, "lessons/") || strings.Contains(entry.Key, "..") || entry.Version == "" || !blockReference.MatchString(entry.SHA256) || entry.BackupKey != date+"/character-assets/"+entry.SHA256+".age" {
			return errors.New("invalid asset reference")
		}
		body, e := decrypt(entry.BackupKey)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != entry.SHA256 {
			return errors.New("protected asset checksum mismatch")
		}
		if e = put(ctx, entry.Key, body); e != nil {
			return e
		}
	}
	return nil
}
