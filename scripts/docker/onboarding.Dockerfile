# syntax=docker/dockerfile:1.20
ARG NODE_VERSION=24
ARG GO_VERSION=1.27.0

FROM node:${NODE_VERSION}-bookworm-slim AS web
WORKDIR /src
# Preserve workspace paths while caching installation independently of source edits.
COPY --parents package.json package-lock.json packages/*/package.json apps/*/package.json examples/*/package.json ./
RUN --mount=type=cache,target=/root/.npm \
    ELECTRON_SKIP_BINARY_DOWNLOAD=1 npm ci --no-audit --no-fund
COPY . .
ARG WHIP_RENDERER_LOCAL_SOURCE
RUN WHIP_RENDERER_LOCAL_SOURCE="$WHIP_RENDERER_LOCAL_SOURCE" npm run build:web \
    && node scripts/pack-web.mjs

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/internal/webassets/dist ./internal/webassets/dist
ARG WHIP_BUILD_VERSION=dev-docker
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=$WHIP_BUILD_VERSION" -o /out/whip ./cmd/whip

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates bash git ripgrep \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --shell /bin/bash whip \
    && mkdir /workspace && chown whip:whip /workspace
COPY --from=build /out/whip /usr/local/bin/whip
COPY --from=web /src/apps/web/renderer-manifest.json /usr/local/share/whip/renderer-manifest.json
COPY --chmod=755 scripts/docker/onboarding-entrypoint.sh /usr/local/bin/onboarding-entrypoint
ENV HOME=/home/whip SHELL=/bin/bash LANG=C.UTF-8 \
    WHIP_HOME=/home/whip/.whip \
    WHIP_NETWORK=1 WHIP_LISTEN=0.0.0.0:4000 \
    WHIP_ALLOWED_HOSTS=localhost:4000,127.0.0.1:4000 \
    WHIP_ALLOWED_ORIGINS=http://localhost:4000,http://127.0.0.1:4000
USER whip
WORKDIR /workspace
RUN git init --quiet \
    && printf '# Onboarding playground\n\nTry asking Whip to create hello.txt or inspect this repository.\n' > README.md \
    && git add README.md \
    && git -c user.name='Whip Test' -c user.email=whip-test@example.invalid commit --quiet -m 'Initial playground'
EXPOSE 4000
ENTRYPOINT ["/usr/local/bin/onboarding-entrypoint"]
