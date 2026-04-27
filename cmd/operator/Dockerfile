FROM --platform=$BUILDPLATFORM golang:1.26@sha256:b54cbf583d390341599d7bcbc062425c081105cc5ef6d170ced98ef9d047c716 AS builder

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

FROM gcr.io/distroless/static:nonroot@sha256:e3f945647ffb95b5839c07038d64f9811adf17308b9121d8a2b87b6a22a80a39
WORKDIR /
COPY --from=builder /workspace/bin/operator .
USER 65532:65532

ENTRYPOINT ["/operator"]
