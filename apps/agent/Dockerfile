# syntax=docker/dockerfile:1.7

FROM golang:1.23-bookworm AS builder
WORKDIR /workspace

# api module is referenced via local replace; copy both modules into place.
COPY api/ /workspace/api/
COPY apps/agent/ /workspace/apps/agent/

WORKDIR /workspace/apps/agent
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -o /out/agent ./cmd/agent

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/agent /usr/local/bin/agent
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/agent"]
