# syntax=docker/dockerfile:1.7
#
# Off-site backup sidecar: pg_dump (custom format) | age (public-key
# encryption) | rclone (R2, any S3-compatible store or any rclone remote).
# Entry point: backupctl (run | backup | list | restore | verify | prune |
# keygen | health). See docs/backup.md.
#
# The build context is this directory, so the image builds from a git URL:
#   docker build -t baoopn-backup https://github.com/baoopn/baoopn-2026.git#main:deploy/backup
#
# Alpine + postgresql17-client instead of postgres:17-alpine: same pg_dump
# major as the server, without the server, its LLVM JIT libraries and the
# inherited VOLUME (which would leave an anonymous volume per container).

ARG ALPINE_VERSION=3.24
FROM alpine:${ALPINE_VERSION}

ARG VERSION=dev
ENV BACKUP_VERSION=${VERSION}
LABEL org.opencontainers.image.title="baoopn-backup" \
      org.opencontainers.image.description="Encrypted off-site PostgreSQL backups (pg_dump + age + rclone) for baoopn-2026" \
      org.opencontainers.image.source="https://github.com/baoopn/baoopn-2026" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache postgresql17-client rclone age ca-certificates tzdata \
 && addgroup -S -g 10001 backup \
 && adduser -S -D -H -u 10001 -G backup -h /nonexistent -s /sbin/nologin backup \
 && mkdir -p /backups /restore \
 && chown backup:backup /backups /restore

COPY --chmod=0755 bin/ /usr/local/bin/

# rclone is configured from env only (RCLONE_CONFIG_BACKUP_*); /dev/null keeps
# its config in memory.
ENV RCLONE_CONFIG=/dev/null TZ=UTC

USER backup:backup
WORKDIR /restore

HEALTHCHECK --interval=5m --timeout=10s --start-period=1m --start-interval=5s --retries=2 \
  CMD ["/usr/local/bin/backupctl", "health"]

ENTRYPOINT ["/usr/local/bin/backupctl"]
CMD ["run"]
