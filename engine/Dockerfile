# syntax=docker/dockerfile:1.7
FROM rust:1.99-bookworm AS chef
RUN cargo install cargo-chef --locked && apt-get update && apt-get install -y --no-install-recommends protobuf-compiler && rm -rf /var/lib/apt/lists/*
WORKDIR /app/engine
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
COPY --from=builder /app/engine/target/release/duolify-engine /duolify-engine
EXPOSE 50051
ENTRYPOINT ["/duolify-engine"]
