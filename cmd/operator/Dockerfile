FROM --platform=$BUILDPLATFORM golang:1.25 AS builder

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

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/bin/operator .
USER 65532:65532

ENTRYPOINT ["/operator"]
