FROM --platform=$BUILDPLATFORM golang:1.26@sha256:5f3787b7f902c07c7ec4f3aa91a301a3eda8133aa32661a3b3a3a86ab3a68a36 AS builder

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

FROM gcr.io/distroless/static:nonroot@sha256:e3f945647ffb95b5839c07038d64f9811adf17308b9121d8a2b87b6a22a80a39
WORKDIR /
COPY --from=builder /workspace/bin/kcp-manager .
USER 65532:65532

ENTRYPOINT ["/kcp-manager"]
