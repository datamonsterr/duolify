// Command rewrite-asset-metadata sets the Content-Type and Cache-Control of existing lesson
// objects (keys under lessons/) that were written without them. It is idempotent and
// defaults to a dry run; pass -apply to rewrite. Credentials and bucket come from the
// same S3_* variables as the API.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/datamonsterr/duolify/services/api/internal/player"
)

func main() {
	apply := flag.Bool("apply", false, "rewrite metadata (default is a dry run)")
	prefix := flag.String("prefix", "lessons/", "key prefix to scan")
	flag.Parse()
	ctx := context.Background()
	store := player.NewS3(os.Getenv)
	keys, err := store.List(ctx, *prefix)
	if err != nil {
		log.Fatal(err)
	}
	changed := 0
	for _, key := range keys {
		ok, err := store.RewriteMetadata(ctx, key, !*apply)
		if err != nil {
			log.Fatalf("%s: %v", key, err)
		}
		if ok {
			changed++
			fmt.Println(key)
		}
	}
	fmt.Printf("%d of %d objects need a metadata rewrite (apply=%v)\n", changed, len(keys), *apply)
}
