FROM --platform=$BUILDPLATFORM golang:1.26@sha256:6df14f4a4bc9d979a3721f488981e0d1b318006377e473ed23d026796f5f4c0a AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-operator \
    go mod download && go mod verify

COPY . ./
RUN --mount=type=cache,target=/go/pkg/mod,id=gomod-operator \
    --mount=type=cache,target=/root/.cache/go-build,id=gobuild-operator \
    CGO_ENABLED=0 \
    GOCACHE=/root/.cache/go-build \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    go build -o ./bin/operator ./cmd/operator

FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240
WORKDIR /
COPY --from=builder /workspace/bin/operator .
USER 65532:65532

ENTRYPOINT ["/operator"]
