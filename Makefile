# Tripvault build, quality gates, container images and publishing.
#
# The test stand is not driven from here: start it with
# "cd tests && docker compose up -d".
#
#   make                  build the backend binary and the frontend bundle
#   make lint             static checks
#   make test             unit tests
#   make images           single-architecture images for local use
#   make push             multi-architecture images, built and published

# The VERSION file is the only place the product version lives.
VERSION ?= $(shell cat VERSION)

REGISTRY        ?= ghcr.io
IMAGE_NAMESPACE ?= nir0k
BACKEND_IMAGE   ?= $(REGISTRY)/$(IMAGE_NAMESPACE)/tripvault-backend
FRONTEND_IMAGE  ?= $(REGISTRY)/$(IMAGE_NAMESPACE)/tripvault-frontend
# LATEST_TAG may be repointed (e.g. edge) or emptied: make push LATEST_TAG=
LATEST_TAG      ?= latest

# linux/arm64 covers a Raspberry Pi on a 64-bit OS. Both Dockerfiles
# cross-compile, so no emulation is needed.
PLATFORMS ?= linux/amd64,linux/arm64

LDFLAGS := -s -w -X github.com/nir0k/tripvault/backend/internal/telemetry.Version=$(VERSION)

IMAGE_BUILD_ARGS := --build-arg VERSION=$(VERSION)

# tag_args expands to "-t image:VERSION [-t image:LATEST_TAG]".
tag_args = -t $(1):$(VERSION) $(if $(LATEST_TAG),-t $(1):$(LATEST_TAG),)

.PHONY: build build-backend build-frontend deps-frontend clean \
        test test-backend test-frontend lint lint-backend lint-frontend \
        images image-backend image-frontend push push-backend push-frontend

build: build-backend build-frontend

build-backend:
	mkdir -p bin
	cd backend && CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" \
		-o ../bin/tripvault-backend ./cmd/tripvault-backend

build-frontend: deps-frontend
	cd frontend && VITE_APP_VERSION=$(VERSION) npm run build

# npm ci keeps the lockfile authoritative.
deps-frontend:
	cd frontend && npm ci --no-audit --no-fund

clean:
	rm -rf bin frontend/dist

test: test-backend test-frontend

test-backend:
	cd backend && go test -race -cover ./...

test-frontend: deps-frontend
	cd frontend && npm test

lint: lint-backend lint-frontend

# golangci-lint is optional locally; go vet and gofmt always run.
lint-backend:
	cd backend && go vet ./...
	if command -v golangci-lint >/dev/null 2>&1; then cd backend && golangci-lint run ./...; fi
	test -z "$$(gofmt -l backend)" || { gofmt -l backend; exit 1; }

lint-frontend: deps-frontend
	cd frontend && npm run typecheck
	cd frontend && npm run lint

images: image-backend image-frontend

image-backend:
	docker build $(IMAGE_BUILD_ARGS) $(call tag_args,$(BACKEND_IMAGE)) backend

image-frontend:
	docker build $(IMAGE_BUILD_ARGS) $(call tag_args,$(FRONTEND_IMAGE)) frontend

# buildx builds and publishes in one step, so a stale image can never go out
# under a fresh tag, and both tags always describe the same build.
push: push-backend push-frontend

push-backend:
	docker buildx build --platform $(PLATFORMS) --push \
		$(IMAGE_BUILD_ARGS) $(call tag_args,$(BACKEND_IMAGE)) backend

push-frontend:
	docker buildx build --platform $(PLATFORMS) --push \
		$(IMAGE_BUILD_ARGS) $(call tag_args,$(FRONTEND_IMAGE)) frontend
