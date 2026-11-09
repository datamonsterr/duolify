# Design

The worker schedules at 02:00 UTC and catches up on startup. River persists the
UTC date in job args, so retries across midnight keep the original object name.
The schema version is the latest applied Goose migration. A successful existing
object is skipped; one complete conditional PutObject publishes ciphertext.
Dump errors never publish. Only encrypted bytes touch a private temporary file,
which is removed on every return. Objects are bounded below S3's single-put limit.
An age X25519 public recipient is sufficient for writers; private keys stay with
recovery operators. External dump and object storage operations are interfaces.
A schema-version change detected during the dump fails publication and retries.
The packaged pg_dump uses explicit libpq environment fields including PGUSER,
so numeric non-root UIDs work without exposing connection secrets in argv.

Dedicated local IAM writer credentials can inspect and put backup objects but
cannot delete objects or administer buckets. A separate administrator owns
retention. Local initialization also confines the API to its application bucket;
it receives neither root/writer credentials nor the encryption key.
No new product tables are introduced. River jobs contain only dates.

System events use the parent-approved reserved operations UUID, web platform,
build version and source=api; a global analytics opt-out suppresses delivery.
Only size, duration and outcome are emitted, never database errors or contents.

This is a synthetic local proof. Production keys and automated 14-day lifecycle
are configured in their separate tickets; ledger-specific 35-day retention from
ADR 0011 is a separate policy, never permission granted to this writer.

Dependency APIs were checked against current official documentation before use:
[PostgreSQL pg_dump](https://www.postgresql.org/docs/18/app-pgdump.html),
[libpq environment](https://www.postgresql.org/docs/18/libpq-envars.html),
[River periodic jobs](https://pkg.go.dev/github.com/riverqueue/river#NewPeriodicJob),
[age](https://pkg.go.dev/filippo.io/age),
[S3 PutObject](https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutObject.html),
[AWS Go SDK](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/s3),
[RustFS IAM](https://docs.rustfs.com/en/security-compliance/iam), and
[madmin](https://pkg.go.dev/github.com/minio/madmin-go/v3).
