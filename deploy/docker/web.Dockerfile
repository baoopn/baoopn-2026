# syntax=docker/dockerfile:1.7
#
# The SvelteKit site (web/, adapter-node). Build context: the repository root.
#   docker build -f deploy/docker/web.Dockerfile .
#
# It is the only public service: it serves the site and proxies /api/* and
# /media/* to the API (API_URL) on the internal network.

ARG NODE_VERSION=22
ARG PNPM_VERSION=11.1.1

FROM node:${NODE_VERSION}-alpine AS base
ARG PNPM_VERSION
ENV PNPM_HOME=/pnpm PATH=/pnpm:$PATH CI=true
RUN corepack enable && corepack prepare pnpm@${PNPM_VERSION} --activate
WORKDIR /app

# ─── Build ────────────────────────────────────────────────────────────────────
FROM base AS build
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# ─── Production dependencies (adapter-node keeps `dependencies` external) ────
FROM base AS prod-deps
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --prod --frozen-lockfile

# ─── Runtime ──────────────────────────────────────────────────────────────────
FROM node:${NODE_VERSION}-alpine
LABEL org.opencontainers.image.title="baoopn-web" \
      org.opencontainers.image.description="baoopn.com site (SvelteKit, adapter-node)" \
      org.opencontainers.image.source="https://github.com/baoopn/baoopn-2026"
WORKDIR /app
# Behind Coolify's Traefik (and Cloudflare): the compose file sets ORIGIN; the
# *_HEADER values are adapter-node's fallbacks when it is unset. BODY_SIZE_LIMIT
# lets proxied uploads (MAX_UPLOAD_MB) and inbound mail (10 MiB) through.
ENV NODE_ENV=production \
    HOST=0.0.0.0 \
    PORT=3000 \
    PROTOCOL_HEADER=x-forwarded-proto \
    HOST_HEADER=x-forwarded-host \
    ADDRESS_HEADER=x-forwarded-for \
    XFF_DEPTH=1 \
    BODY_SIZE_LIMIT=16M

COPY --from=prod-deps --chown=node:node /app/node_modules ./node_modules
COPY --from=build --chown=node:node /app/build ./build
COPY --chown=node:node web/package.json ./

USER node
EXPOSE 3000

# robots.txt is built in the web server, so the probe does not depend on the API being up.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:3000/robots.txt || exit 1

CMD ["node", "build"]
