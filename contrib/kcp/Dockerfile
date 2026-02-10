FROM --platform=$BUILDPLATFORM golang:1.25 AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-kcp \
    go mod download && go mod verify

COPY contrib/kcp/go.mod contrib/kcp/go.sum ./contrib/kcp/
RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-kcp \
    cd contrib/kcp && \
    go mod download && go mod verify

COPY . ./
RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-kcp \
    --mount=type=cache,target=/root/.cache/go-build,id=gobuild-kcp \
    cd ./contrib/kcp && \
    CGO_ENABLED=0 \
    GOCACHE=/root/.cache/go-build \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    go build -o ../../bin/kcp-manager ./cmd

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/bin/kcp-manager .
USER 65532:65532

ENTRYPOINT ["/kcp-manager"]
