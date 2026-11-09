# Engine

Private stateless gRPC service. Run `mise run engine:run` with `ENGINE_PROVIDER_MODE=mock` for local development. See the repository `.env.example` for settings and `evals/README.md` for evaluation.

Build the image from the repository root, so the protobuf contract is available: `docker build -f engine/Dockerfile .`.
