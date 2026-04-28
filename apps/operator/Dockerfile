# syntax=docker/dockerfile:1.7

FROM golang:1.23-bookworm AS builder
WORKDIR /workspace

COPY api/ /workspace/api/
COPY apps/operator/ /workspace/apps/operator/

WORKDIR /workspace/apps/operator
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -o /out/operator ./cmd/operator

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/operator /usr/local/bin/operator
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/operator"]
