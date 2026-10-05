# Tripvault

Tripvault is a self-hosted service for planning trips day by day and writing them up afterwards. It is built for one household or a small group of friends: nobody signs up on their own, an administrator creates the accounts, and every trip is shared only with the people and links its owner chooses.

## Features

- **Plans.** Days of places and activities in the order they are reached, with stays, flights, trains and ferries, legs between places routed on real roads, times, costs and notes. A route drawn in Google Maps can be brought in from its link, and a GPX or KML track can be attached to an activity.
- **Reports.** A report is a trip of its own, written from scratch or copied from a plan: what was visited, the story of each day and place, ratings, what was actually spent, and the photographs. A report can be translated into further languages.
- **Photographs.** Uploaded from a phone or a computer, shrunk in the browser, ordered by when they were taken, with favourites, covers and private pictures.
- **Budget.** Planned and actual costs by category, who paid, and who owes whom.
- **Packing list** for each plan, filled from ready-made templates.
- **Ideas** of where to go one day: countries, best months, rough costs and ways of getting there.
- **Sharing.** Members with the roles of editor or viewer, and read-only links that need no account.
- **PDF.** A report prints as a travel journal with maps; a plan prints as a reference for the road with QR codes for every place; a packing list prints as a checklist.
- **Backups** of the whole instance to the server's own disk or over SFTP, optionally encrypted, on a schedule, with restore from the interface.
- The interface is available in English and Russian.

## Quick start

Tripvault runs as three containers - PostgreSQL, the API and the web interface - from published images for `linux/amd64` and `linux/arm64`. You need Docker with the Compose plugin.

```sh
mkdir tripvault && cd tripvault
curl -fsSLO https://raw.githubusercontent.com/nir0k/Tripvault/master/docker-compose.yaml
curl -fsSL -o .env https://raw.githubusercontent.com/nir0k/Tripvault/master/.env.example
```

Fill in `.env`. Four values are needed for a first start:

```sh
POSTGRES_PASSWORD=...           # openssl rand -base64 24
TRIPVAULT_AUTH_JWT_SECRET=...   # openssl rand -base64 48
TRIPVAULT_ADMIN_EMAIL=you@example.com
TRIPVAULT_ADMIN_PASSWORD=...    # at least 8 characters
```

Then start it:

```sh
docker compose up -d
```

The interface listens on `http://127.0.0.1:8080`. Sign in with the administrator's email and password, then remove `TRIPVAULT_ADMIN_PASSWORD` from `.env`: the account exists from now on, and later changes to these two values do nothing.

The port is bound to localhost on purpose. To reach Tripvault from other devices, put a reverse proxy with TLS or a tunnel in front of it - see [Deployment](docs/deployment.md#reverse-proxy-and-tls).

Road routes and place search need a free [openrouteservice](https://account.heigit.org) key in `TRIPVAULT_ROUTING_API_KEY` and `TRIPVAULT_GEOCODING_API_KEY`, or servers of your own. Without them legs are straight-line estimates, and places are added by coordinates, a map link or a click on the map.

## Upgrading

```sh
docker compose pull
docker compose up -d
```

Database migrations are applied automatically when the backend starts. Set `TRIPVAULT_VERSION` in `.env` to a release such as `1.10.1` to decide when upgrades happen instead of following `latest`. Take a backup before upgrading: an archive cannot be restored into a build older than the one that wrote it.

## Documentation

- [Deployment](docs/deployment.md) - configuration, reverse proxy, map tiles, routing and place search, volumes, security.
- [Backups and restore](docs/backups.md) - destinations, schedules, encryption, restoring from the interface or the command line.
- [Colour themes](docs/themes.md) - the theme file and what each of its colours is for.
- [Development](docs/development.md) - building from source, tests, the test stand, migrations and releases.
- The API reference is served by every instance at `/docs`, and the OpenAPI document at `/openapi.yaml`.

## Versioning

The product version lives in the [`VERSION`](VERSION) file and stamps the backend, the web interface, the API reference and both images. Images are published as `ghcr.io/nir0k/tripvault-backend` and `ghcr.io/nir0k/tripvault-frontend`, tagged with the version and `latest`.

## License

Tripvault is licensed under the [Apache License 2.0](LICENSE). Map data and tiles belong to their providers and are credited in the interface; the photographs of the test stand are credited in [`tests/seed/fixtures/photos/CREDITS.md`](tests/seed/fixtures/photos/CREDITS.md).
