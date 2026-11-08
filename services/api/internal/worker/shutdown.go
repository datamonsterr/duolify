package worker

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

func stop(client *river.Client[pgx.Tx]) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Stop(ctx); err != nil {
		hardCtx, hardCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer hardCancel()
		return errors.Join(err, client.StopAndCancel(hardCtx))
	}
	return nil
}
