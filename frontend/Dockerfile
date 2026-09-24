# syntax=docker/dockerfile:1.19

# Build stage, pinned to the build host's platform. The bundle is static and
# architecture-independent, so Node never runs on the target architecture.
FROM --platform=$BUILDPLATFORM node:24.21.0-alpine AS build

ARG VERSION=dev

WORKDIR /src

COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund

COPY . .

# The product version is baked into the bundle, so the interface reports the
# same version as the backend and the OpenAPI document.
ENV VITE_APP_VERSION=${VERSION}
RUN npm run build

# Runtime stage: COPY and metadata only, no RUN.
FROM nginx:1.31.6-alpine AS runtime

ARG VERSION=dev

LABEL org.opencontainers.image.title="tripvault-frontend" \
      org.opencontainers.image.description="Tripvault trip planning service web interface" \
      org.opencontainers.image.source="https://github.com/nir0k/tripvault" \
      org.opencontainers.image.version="${VERSION}"

# The API upstream is filled in from the environment when the container
# starts. NGINX_ENVSUBST_FILTER limits substitution to TRIPVAULT_* variables so
# nginx's own $host, $remote_addr and the like survive templating.
# TRIPVAULT_MAP_TILE_ORIGIN is empty unless the operator runs their own tile
# server on plain http: the policy then names that one origin, for images only.
# TRIPVAULT_TRUSTED_PROXY is the proxy in front whose X-Forwarded-For is
# believed; the loopback default believes none.
ENV TRIPVAULT_API_UPSTREAM=tripvault-backend:8080 \
    TRIPVAULT_MAP_TILE_ORIGIN="" \
    TRIPVAULT_TRUSTED_PROXY=127.0.0.1/32 \
    NGINX_ENVSUBST_FILTER="TRIPVAULT_"

COPY --from=build /src/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/templates/default.conf.template

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
    CMD wget -q -O- http://127.0.0.1:8080/nginx-health >/dev/null 2>&1 || exit 1
