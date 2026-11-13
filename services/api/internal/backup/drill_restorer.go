package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os/exec"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var containerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// DockerRestorer restores into a fresh database of a disposable Postgres
// container using `docker exec`, so no host Postgres client tools are needed.
// AdminURL must reach that container's loopback-published port.
type DockerRestorer struct {
	Container string
	AdminURL  string
	User      string
}

// Restore creates a uniquely named database, loads the dump (stdin, never argv)
// and returns a pool and a cleanup that drops the database.
func (r DockerRestorer) Restore(ctx context.Context, dump []byte) (*pgxpool.Pool, func(), error) {
	if !containerName.MatchString(r.Container) || !containerName.MatchString(r.User) {
		return nil, nil, errors.New("invalid drill container name")
	}
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return nil, nil, err
	}
	name := "drill_" + hex.EncodeToString(suffix)
	run := func(stdin []byte, args ...string) error {
		cmd := exec.CommandContext( //nolint:gosec // container and user validated by containerName; dump goes through stdin
			ctx, "docker", append([]string{"exec", "-i", r.Container}, args...)...)
		cmd.Stdin = bytes.NewReader(stdin)
		return cmd.Run()
	}
	drop := func() {
		// Cleanup must outlive a cancelled drill context.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		_ = exec.CommandContext(cleanup, "docker", "exec", r.Container, "dropdb", "-U", r.User, "--force", "--if-exists", name).Run() //nolint:gosec // validated identifiers
	}
	if err := run(nil, "createdb", "-U", r.User, name); err != nil {
		return nil, nil, errors.New("drill createdb failed")
	}
	if err := run(dump, "pg_restore", "--exit-on-error", "--no-owner", "--no-acl", "-U", r.User, "-d", name); err != nil {
		drop()
		return nil, nil, errors.New("drill pg_restore failed")
	}
	u, err := url.Parse(r.AdminURL)
	if err != nil {
		drop()
		return nil, nil, errors.New("invalid drill database URL")
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		drop()
		return nil, nil, errors.New("drill connection failed")
	}
	return pool, func() { pool.Close(); drop() }, nil
}
