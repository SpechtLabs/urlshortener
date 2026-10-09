# urlshortener container image: the controller, the redirect server and the
# API in one binary, with the HTML templates it renders next to it.
#
#   docker build -t urlshortener:dev .    (or: mise run image)

# Keep in lockstep with go in .mise.toml; Renovate bumps both together.
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.27.2 AS builder

WORKDIR /src

# The module files first, so the download layer survives source edits.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY api/ ./api/
COPY cmd/ ./cmd/
COPY docs/ ./docs/
COPY internal/ ./internal/
COPY pkg/ ./pkg/

ARG TARGETOS
ARG TARGETARCH

# Cross-compiled on the build platform, so the multi-arch build doesn't run
# the Go toolchain under emulation. CGO is off because the runtime image has
# no libc.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w" -o /out/urlshortener ./cmd

FROM gcr.io/distroless/static-debian12:nonroot

# The server reads its templates and assets from html/ in the working
# directory.
WORKDIR /
COPY html/ ./html/
COPY --from=builder /out/urlshortener /urlshortener

USER 65532:65532

LABEL org.opencontainers.image.title="urlshortener"
LABEL org.opencontainers.image.description="A URL shortener and redirect controller for Kubernetes"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.vendor="SpechtLabs"

ENTRYPOINT ["/urlshortener"]
