# syntax=docker/dockerfile:1.7
#
# The Go API (cmd/api). Build context: the repository root.
#   docker build -f deploy/docker/api.Dockerfile --build-arg VERSION=$(git rev-parse --short HEAD) .

ARG GO_VERSION=1.27

# ─── Build ────────────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Go sources, migrations and seed content (both embedded in the binary).
# api.Dockerfile.dockerignore keeps web/, workers/ and local data out.
COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -ldflags="-s -w -X github.com/baoopn/baoopn-2026/internal/platform/app.Version=${VERSION}" \
      -o /out/api ./cmd/api \
 && mkdir -p /data/media

# ─── Runtime ──────────────────────────────────────────────────────────────────
# distroless static, non-root (uid 65532). The binary embeds time/tzdata, so
# no zoneinfo is needed in the image.
FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION=dev
LABEL org.opencontainers.image.title="baoopn-api" \
      org.opencontainers.image.description="baoopn.com backend: public API, admin API, webhooks, media" \
      org.opencontainers.image.source="https://github.com/baoopn/baoopn-2026" \
      org.opencontainers.image.version="${VERSION}"

COPY --from=build /out/api /usr/local/bin/api
# Uploaded media lives here; mount a volume on it. A fresh named volume is
# initialised from this directory, so it is owned by the non-root user.
COPY --from=build --chown=65532:65532 /data/media /data/media

ENV MEDIA_DIR=/data/media HTTP_ADDR=:8080
USER nonroot:nonroot
EXPOSE 8080
VOLUME ["/data/media"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/usr/local/bin/api", "healthcheck"]

ENTRYPOINT []
CMD ["/usr/local/bin/api"]
