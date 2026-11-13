# syntax=docker/dockerfile:1
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/backup-init ./cmd/backup-init

# The public recipient is runtime config; recovery private keys never enter images.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOBIN=/out go install filippo.io/age/cmd/age@v1.3.2 && \
    CGO_ENABLED=0 GOBIN=/out go install filippo.io/age/cmd/age-keygen@v1.3.2

# Pin pg_dump/pg_restore 18.6 and their runtime libraries together by digest.
FROM postgres:18@sha256:e3d24f6906d0760bf4da0dae872415391ededefad23fd471c47dc53b0ddf3c42 AS worker
COPY --from=build /out/api /api
COPY --from=build /out/age /out/age-keygen /usr/local/bin/
USER 65532:65532
ENTRYPOINT ["/api"]
CMD ["worker"]

# Only this development initializer contains the RustFS administration client.
FROM gcr.io/distroless/static-debian13:nonroot AS backup-init
COPY --from=build /out/backup-init /backup-init
USER 65532:65532
ENTRYPOINT ["/backup-init"]

FROM gcr.io/distroless/static-debian13:nonroot AS api
COPY --from=build /out/api /api
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/api"]
CMD ["serve"]
