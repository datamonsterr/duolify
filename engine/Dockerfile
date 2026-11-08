# syntax=docker/dockerfile:1.7
FROM rust:1.99-bookworm AS chef
RUN cargo install cargo-chef --locked && apt-get update && apt-get install -y --no-install-recommends protobuf-compiler && rm -rf /var/lib/apt/lists/*
WORKDIR /app/engine
FROM chef AS probe
ARG TARGETARCH
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
# Upstream v0.4.61 checksums.txt; support both desktop build architectures.
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) checksum=3746f63e89561616ce69eafdc9b46f8b23b47db3a564f6f2e8c31359a60e2f38 ;; \
      arm64) checksum=c6ede27c8da07748f188930dfffba089e058cf711e26604591a152ff225d3a99 ;; \
      *) echo "Unsupported probe architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    curl --fail --location --silent --show-error \
      "https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.61/grpc_health_probe-linux-${TARGETARCH}" \
      --output /grpc_health_probe; \
    echo "$checksum  /grpc_health_probe" | sha256sum --check; \
    chmod 755 /grpc_health_probe
FROM chef AS planner
COPY engine/ .
COPY proto/ /app/proto/
RUN cargo chef prepare --recipe-path recipe.json
FROM chef AS builder
COPY --from=planner /app/engine/recipe.json recipe.json
COPY proto/ /app/proto/
RUN --mount=type=cache,target=/usr/local/cargo/registry cargo chef cook --release --recipe-path recipe.json
COPY engine/ .
RUN --mount=type=cache,target=/usr/local/cargo/registry cargo build --release --locked -p engine-server --bin duolify-engine
FROM gcr.io/distroless/cc-debian13:nonroot
COPY --from=probe /grpc_health_probe /grpc_health_probe
COPY --from=builder /app/engine/target/release/duolify-engine /duolify-engine
EXPOSE 50051
ENTRYPOINT ["/duolify-engine"]
