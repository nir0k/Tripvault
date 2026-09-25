# syntax=docker/dockerfile:1.19

# Build stage, pinned to the build host's platform: the Go toolchain runs
# natively and cross-compiles to the target architecture, so an arm64 image
# builds on an amd64 host without QEMU.
#
# Previews are rendered by libvips through cgo, so the C part is cross-compiled
# too: clang targets the architecture of the image, and the headers and
# libraries it builds against are Alpine's own packages for that architecture,
# unpacked into a root of their own. apk only unpacks them - --no-scripts keeps
# it from running anything built for the target - so nothing is emulated.
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine AS build

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
# The Alpine branch the libraries come from. It must be the branch of the
# runtime image below, whose C library they are linked against.
ARG ALPINE_BRANCH=v3.24

RUN apk add --no-cache clang lld pkgconf alpine-keys

# /sysroot holds what the build compiles and links against, /out/libroot what
# the running service loads. The target's package architecture is written down
# for the build step.
RUN case "${TARGETARCH}" in \
      amd64) arch=x86_64 ;; \
      arm64) arch=aarch64 ;; \
      *) echo "unsupported architecture ${TARGETARCH}" >&2; exit 1 ;; \
    esac && \
    echo "${arch}" > /apk-arch && \
    mkdir -p /sysroot /out/libroot && \
    repos="--repository https://dl-cdn.alpinelinux.org/alpine/${ALPINE_BRANCH}/main --repository https://dl-cdn.alpinelinux.org/alpine/${ALPINE_BRANCH}/community" && \
    apk add --root /sysroot --arch "${arch}" --initdb --no-scripts --no-cache \
      --keys-dir "/usr/share/apk/keys/${arch}" ${repos} vips-dev musl-dev gcc && \
    apk add --root /out/libroot --arch "${arch}" --initdb --no-scripts --no-cache \
      --keys-dir "/usr/share/apk/keys/${arch}" ${repos} vips

WORKDIR /src

# Modules are downloaded before the source is copied, so a code-only change
# reuses the cached layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# A position-independent executable is what Alpine's toolchain links by
# default, and lld is the linker that knows every target clang does.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    arch="$(cat /apk-arch)" && \
    CC="clang --target=${arch}-alpine-linux-musl --sysroot=/sysroot" \
    CGO_LDFLAGS="-fuse-ld=lld" \
    PKG_CONFIG_SYSROOT_DIR=/sysroot \
    PKG_CONFIG_LIBDIR=/sysroot/usr/lib/pkgconfig:/sysroot/usr/share/pkgconfig \
    CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -buildmode=pie -trimpath \
      -ldflags="-s -w -X github.com/nir0k/tripvault/backend/internal/telemetry.Version=${VERSION}" \
      -o /out/tripvault-backend ./cmd/tripvault-backend

# The runtime file tree is prepared here, so the runtime stage needs only COPY.
# The directories are part of it: a container runs as an ordinary user and
# cannot create one under /, and an empty volume mounted over one of these
# inherits its owner. media holds the uploaded files, backups the archives, and
# config the key that seals stored credentials.
RUN mkdir -p /out/rootfs/app/media /out/rootfs/app/backups /out/rootfs/app/config

# Runtime stage: COPY and metadata only, without a single RUN, so nothing built
# for the target architecture executes while the image is assembled. The CA
# bundle comes from the build stage, the time zone database is embedded in the
# binary, libvips and the libraries it needs come from the root unpacked for
# this architecture, and the health check uses the base image's busybox wget.
FROM alpine:3.24.1 AS runtime

ARG VERSION=dev

LABEL org.opencontainers.image.title="tripvault-backend" \
      org.opencontainers.image.description="Tripvault trip planning service API" \
      org.opencontainers.image.source="https://github.com/nir0k/tripvault" \
      org.opencontainers.image.version="${VERSION}"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/libroot/usr/lib/ /usr/lib/
COPY --from=build /out/tripvault-backend /usr/local/bin/tripvault-backend
COPY --from=build --chown=10001:10001 /out/rootfs/app /app

# A numeric user needs no passwd entry, which would take a RUN to create.
USER 10001:10001
WORKDIR /app

ENV TRIPVAULT_HTTP_ADDR=:8080

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=5 \
    CMD wget -q -O- http://127.0.0.1:8080/healthz >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/usr/local/bin/tripvault-backend"]
