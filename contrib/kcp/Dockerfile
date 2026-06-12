FROM --platform=$BUILDPLATFORM golang:1.26@sha256:87a41d2539e5671777734e91f467499ed5eafb1fb1f77221dff2744db7a51775 AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-kcp \
    go mod download && go mod verify

COPY contrib/kcp/go.mod contrib/kcp/go.sum ./contrib/kcp/
RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-kcp \
    cd contrib/kcp && \
    go mod download && go mod verify

COPY . ./

ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-kcp \
    --mount=type=cache,target=/root/.cache/go-build,id=gobuild-kcp \
    cd ./contrib/kcp && \
    CGO_ENABLED=0 \
    GOCACHE=/root/.cache/go-build \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    go build -ldflags "-X github.com/platform-mesh/resource-broker/pkg/version.GitCommit=${GIT_COMMIT} -X github.com/platform-mesh/resource-broker/pkg/version.BuildTime=${BUILD_TIME}" -o ../../bin/kcp-manager ./cmd

FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240
WORKDIR /
COPY --from=builder /workspace/bin/kcp-manager .
USER 65532:65532

ENTRYPOINT ["/kcp-manager"]
