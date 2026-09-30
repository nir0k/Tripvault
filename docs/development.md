# Development

This guide is for working on Tripvault's source: building it, testing it, running it locally and releasing it.

## Repository layout

| Path | What it is |
|---|---|
| `backend/` | The Go API service, module `github.com/nir0k/tripvault/backend`, with its own `go.mod`. The binary is `backend/cmd/tripvault-backend`. |
| `backend/internal/httpapi` | Routes, handlers, middleware; the OpenAPI document and the Scalar reader in `docs/`. |
| `backend/internal/domain` | Types and business rules, free of storage and HTTP. |
| `backend/internal/storage` | PostgreSQL repositories (`postgres/`) and the embedded migrations (`migrations/sql`). |
| `backend/internal/media` | The file store, type detection and previews rendered by libvips. |
| `backend/internal/pdf` | The PDF documents: a report's journal, a plan's reference, a packing list. |
| `backend/internal/backup` | Archives, destinations, encryption and restore. |
| `frontend/` | The single-page interface: Vue 3, TypeScript, Vite, Pinia, vue-router, vue-i18n, Tailwind CSS and DaisyUI. |
| `frontend/src/i18n` | The interface's dictionaries, `en.json` and `ru.json`. |
| `tests/` | The disposable test stand and the fixtures it is seeded with. |
| `docs/` | This documentation. |
| `VERSION` | The product version. |
| `docker-compose.yaml`, `.env.example` | The production deployment. |

## Prerequisites

- Go 1.27 with cgo, and the libvips headers: `vips-devel` on Fedora, `libvips-dev` on Debian and Ubuntu. Previews are rendered by libvips, so the backend does not build or test without them. The images bring their own.
- Node.js 22 or newer.
- Docker with the Compose plugin and buildx, for the images and the test stand.
- Optionally `golangci-lint`, which `make lint` runs when it is installed.

## Make targets

```sh
make              # build bin/tripvault-backend and frontend/dist
make lint         # go vet, golangci-lint if present, gofmt, vue-tsc, eslint, locale check
make test         # backend tests with the race detector, frontend tests with vitest
make images       # single-architecture images for local use
make push         # multi-architecture images, built and published
make clean        # remove the build output
```

## The test stand

`tests/docker-compose.yaml` builds the current source and runs it with examples, for checking changes by hand. It is not driven through the Makefile:

```sh
cd tests
docker compose up -d      # build the working tree and start
docker compose down       # stop and destroy all of its state
```

- The interface is at `http://127.0.0.1:8071`, and the API directly at `http://127.0.0.1:8070`. The API reference is at `/docs` on either.
- The stand has no volumes of any kind: its database, photographs and backups live inside the containers and disappear with them.
- Every plain `docker compose up -d` rebuilds the images from the working tree; unchanged layers come from the build cache.
- `tests/.env` is committed and holds throwaway values only. It binds the stand to localhost; set `TRIPVAULT_BIND_ADDRESS=0.0.0.0` there to open it to a trusted local network. The stand's passwords are published in the source, so never expose it further.

### Examples

Once the backend is healthy, the one-shot `tripvault-seed` service (`tests/seed/seed.py`, Python's standard library only) loads the fixtures in `tests/seed/fixtures` through the public REST API: a trip that was travelled and written up into a report, a plan still being planned, and a few ideas with their tags, together with photographs, tracks and attachments.

- The administrator is `TRIPVAULT_ADMIN_EMAIL` with `TRIPVAULT_ADMIN_PASSWORD` from `tests/.env`.
- `editor@example.com` and `viewer@example.com` share a password printed in the seed's log: `docker compose logs tripvault-seed`.
- A stand that already has these accounts is left alone. `TRIPVAULT_SEED=false` starts an empty one.

The fixtures go through the same validation as anything a person types, so a change to the API that refuses them fails the seed loudly on the step that broke. The backend itself knows nothing about them.

## Running outside Docker

The frontend's dev server proxies `/api` to a backend on `localhost:8080`:

```sh
cd frontend
npm ci
npm run dev
```

The backend then runs from `bin/tripvault-backend` against a PostgreSQL 16 of your own, configured through environment variables:

```sh
export TRIPVAULT_ENV=development
export TRIPVAULT_DB_DSN=postgres://tripvault:tripvault@localhost:5432/tripvault?sslmode=disable
export TRIPVAULT_AUTH_JWT_SECRET=dev-only-secret
export TRIPVAULT_ADMIN_EMAIL=admin@example.com
export TRIPVAULT_ADMIN_PASSWORD=dev-password
export TRIPVAULT_MEDIA_PATH=/tmp/tripvault/media
export TRIPVAULT_BACKUP_PATH=/tmp/tripvault/backups
export TRIPVAULT_SECRETS_KEY_FILE=/tmp/tripvault/secrets.key
make build-backend && bin/tripvault-backend
```

`development` relaxes the checks meant for production, such as the length of the signing secret. Every other variable is described in [Deployment](deployment.md#configuration).

## Conventions

### Languages

Code, comments, logs, API responses and documentation are in English. The interface takes every visible string from its dictionaries, and `en.json` and `ru.json` must hold the same keys, which `make lint` checks. The PDF is written by the backend, so its own words live in `backend/internal/pdf/locale.go`, in both languages.

### Comments

Every function has a comment explaining what it does and why, not a restatement of its name. Exported Go identifiers are documented starting with their name, and exported functions list their arguments and return values:

```go
// FunctionName - brief description of the function.
//
// Arguments:
//   - argName: description of the argument.
//
// Returns:
//   - description of the returned value.
//   - description of the error, if the function can return an error.
```

### API

The OpenAPI document lives in `backend/internal/httpapi/docs/openapi.yaml` and is embedded in the binary. A test checks that every route is documented, so an endpoint is added to the document together with its handler. The reference is served offline by the backend itself, with the Scalar reader, from the address it runs at.

### Settings

A variable the backend reads is described in `.env.example` and passed by both compose files, or listed in the configuration's coverage test among the few left out on purpose. A variable documented but not passed by compose would do nothing when an operator sets it.

### Dependencies

A new dependency, backend or frontend, starts at its latest stable release and is pinned exactly: no `^` or `~` in `package.json`.

## Migrations

Migrations are SQL files in `backend/internal/storage/migrations/sql`, embedded in the binary and applied with goose when the backend starts. There is no separate migration command.

- `00001_baseline.sql` is the schema released as 1.0.0 and is never edited: databases in production were created from it.
- Every change after it is a new numbered file with a working `-- +goose Down` section.
- Migrations added since the last release may be merged into one before the release, since no database outside development has applied them.
- A restore refuses an archive whose schema is newer than the build, so a migration also moves the oldest build a new archive can be restored into.

## Releasing

1. Set the new version in `VERSION`. It is the only place the version lives: it stamps the backend, the frontend bundle, the OpenAPI document and both images.
2. Run `make lint` and `make test`.
3. Commit as `Release Tripvault X.Y.Z: <summary>` with a list of what changed, and push.
4. Publish the images with `make push`. It builds `ghcr.io/nir0k/tripvault-backend` and `ghcr.io/nir0k/tripvault-frontend` for `linux/amd64` and `linux/arm64` and tags them with the version and `latest`. Both Dockerfiles cross-compile, so no emulation is needed. `LATEST_TAG=` publishes the version alone, and `REGISTRY` and `IMAGE_NAMESPACE` point it elsewhere.

The runtime stages of the Dockerfiles run no command, so nothing is installed into an image after its build stage.
