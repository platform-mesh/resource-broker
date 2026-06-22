FROM --platform=$BUILDPLATFORM golang:1.26@sha256:792443b89f65105abba56b9bd5e97f680a80074ac62fc844a584212f8c8102c3 AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-operator \
    go mod download && go mod verify

COPY . ./

ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-operator \
    --mount=type=cache,target=/root/.cache/go-build,id=gobuild-operator \
    CGO_ENABLED=0 \
    GOCACHE=/root/.cache/go-build \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    go build -ldflags "-X github.com/platform-mesh/resource-broker/pkg/version.GitCommit=${GIT_COMMIT} -X github.com/platform-mesh/resource-broker/pkg/version.BuildTime=${BUILD_TIME}" -o ./bin/operator ./cmd/operator

FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240
WORKDIR /
COPY --from=builder /workspace/bin/operator .
USER 65532:65532

ENTRYPOINT ["/operator"]
