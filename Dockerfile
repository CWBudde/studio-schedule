# syntax=docker/dockerfile:1
#
# The production image: one static binary with the web UI embedded, on Alpine,
# running as a non-root user. Build it from a checkout with the Git LFS files
# pulled (receipt.wasm, the receipt renderer):
#
#   git lfs pull
#   docker build --build-arg VERSION="$(git describe --tags --always --dirty)" -t studio-schedule .
#
# The web and Go stages run on the build machine's platform and cross-compile,
# so a multi-platform build (buildx --platform linux/amd64,linux/arm64) only
# emulates the few runtime-stage commands. See docs/operations.md.
#
# The base images are written out (no ARGs) so that Renovate updates them; the
# golang image follows go.mod's toolchain line.

# ---------------------------------------------------------------- web UI

FROM --platform=$BUILDPLATFORM node:24-alpine3.24 AS web

WORKDIR /src/web

# pnpm in the version web/package.json pins ("packageManager").
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN npm install --global --no-fund --no-audit \
	"$(node -p 'require("./package.json").packageManager.split("+")[0]')"
RUN --mount=type=cache,id=pnpm-store,target=/root/.local/share/pnpm/store \
	pnpm install --frozen-lockfile

COPY web/ ./
# The production build (HTTP transport), served from the root path.
RUN VITE_BASE=/ pnpm build

# ---------------------------------------------------------------- server

FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine3.24 AS server

WORKDIR /src

# Without `git lfs pull` the checkout holds a small text pointer instead of
# the WebAssembly module; fail right away instead of shipping an image that
# cannot render receipts.
COPY internal/pdf/typstwasm/receipt.wasm /tmp/receipt.wasm
RUN if [ "$(head -c 4 /tmp/receipt.wasm | od -An -tx1 | tr -d ' \n')" != "0061736d" ]; then \
		echo "internal/pdf/typstwasm/receipt.wasm is not a WebAssembly module (a Git LFS pointer?): run 'git lfs pull' before building the image" >&2; \
		exit 1; \
	fi

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd/ cmd/
COPY internal/ internal/
# Embed the built UI the way scripts/build-server.sh does: internal/webui/dist
# is the embed directory.
COPY --from=web /src/web/dist/ internal/webui/dist/

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
	go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
	-o /out/studio-schedule ./cmd/studio-schedule

# ---------------------------------------------------------------- runtime

FROM alpine:3.24

# ca-certificates for SMTP over TLS and PostgreSQL with sslmode=verify-full;
# tzdata for the school time zones.
RUN apk add --no-cache ca-certificates tzdata \
	&& addgroup -S -g 10001 app \
	&& adduser -S -D -H -h /nonexistent -s /sbin/nologin -u 10001 -G app app \
	&& mkdir -p /var/cache/studio-schedule \
	&& chown app:app /var/cache/studio-schedule

COPY --from=server /out/studio-schedule /usr/local/bin/studio-schedule

# XDG_CACHE_HOME holds wazero's compilation cache of the receipt renderer
# (os.UserCacheDir), so a restart does not compile it again. It is a cache and
# may be deleted at any time. No VOLUME: an anonymous volume per container
# would pile up; mount a named one there to keep the cache across containers
# (docker-compose.yml does, see docs/operations.md).
ENV XDG_CACHE_HOME=/var/cache/studio-schedule \
	LOG_FORMAT=json \
	HTTP_ADDR=:8080

USER 10001:10001
EXPOSE 8080

# The binary requests /healthz at the port of HTTP_ADDR, so the check follows
# the listen address.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
	CMD ["studio-schedule", "healthcheck"]

ENTRYPOINT ["studio-schedule"]
CMD ["serve"]
