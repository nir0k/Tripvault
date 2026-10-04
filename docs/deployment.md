# Deployment

This guide is for whoever runs a Tripvault instance. It assumes the [quick start](../README.md#quick-start) from the README: the root `docker-compose.yaml`, published images, and an `.env` made from `.env.example`.

## What runs

| Container | Image | Role |
|---|---|---|
| `tripvault-db` | `postgres:16` | The database. It publishes no port. |
| `tripvault-backend` | `ghcr.io/nir0k/tripvault-backend` | The API, the PDF renderer, previews and backups. It publishes no port. |
| `tripvault-frontend` | `ghcr.io/nir0k/tripvault-frontend` | nginx serving the interface and proxying `/api`, `/docs` and the probes to the backend. It is the only published port. |

The browser sees one origin, so no CORS configuration is needed. The backend applies its database migrations itself on start-up; there is no separate migration step.

## Configuration

Everything is set in `.env` beside `docker-compose.yaml`. The compose file passes each of these variables to the container that reads it. After a change, `docker compose up -d` recreates the containers affected.

### Required

| Variable | Description |
|---|---|
| `POSTGRES_PASSWORD` | The database password. Generate one: `openssl rand -base64 24`. |
| `TRIPVAULT_AUTH_JWT_SECRET` | Signs session tokens. At least 32 characters; the backend refuses to start with a shorter or a well-known one. Anyone who knows it can act as any account, and changing it signs everybody out. Generate one: `openssl rand -base64 48`. |

### First administrator

| Variable | Description |
|---|---|
| `TRIPVAULT_ADMIN_EMAIL` | The first administrator, created only while the database has no accounts. Set both or neither. |
| `TRIPVAULT_ADMIN_PASSWORD` | Their password, at least 8 characters. Remove it from `.env` after the first sign-in; later changes do nothing. |

Further accounts are invited by email from Administration → Users when mail is configured and enabled. Creating an account with a temporary password remains available as a fallback; its owner replaces that password at the first sign-in.

### Email

| Variable | Default | Description |
|---|---|---|
| `TRIPVAULT_MAIL_HOST` | | SMTP host. Leaving it empty leaves mail unconfigured. |
| `TRIPVAULT_MAIL_PORT` | `587` | SMTP port. |
| `TRIPVAULT_MAIL_USERNAME` | | SMTP username. Set it together with the password; authenticated delivery requires TLS. |
| `TRIPVAULT_MAIL_PASSWORD` | | SMTP password. It is read from the environment and is never exposed in the interface or stored in the database. |
| `TRIPVAULT_MAIL_TLS` | `starttls` | `starttls`, `tls` for implicit TLS, or `none` for an explicitly unsecured server without authentication. |
| `TRIPVAULT_MAIL_FROM_ADDRESS` | | Envelope and message sender. Required with the host. |
| `TRIPVAULT_MAIL_FROM_NAME` | `Tripvault` | Display name of the sender. |
| `TRIPVAULT_MAIL_PUBLIC_URL` | | Public HTTPS origin used in recovery and invitation links, such as `https://trips.example.com`. Required with the host. |

After the transport is configured, an administrator enables delivery and may send a test message from Administration → Service status. The switch is stored in the database. Disabling it keeps newly generated messages in the durable queue; they resume after delivery is enabled again. The test action bypasses that switch so configuration can be checked before enabling it.

Password recovery, account invitations and trip invitations appear only while delivery is configured and enabled. Ordinary notifications can be disabled by each person in their profile; password and other security notifications cannot. Tripvault does not offer passwordless sign-in.

An administrator may also allow self-registration under Administration → Status. It can be switched on only while SMTP is configured and delivery is enabled, and it closes by itself while delivery is off. A self-registered account stays inactive until its address is confirmed, either by the link in the confirmation message or by typing its six-digit code on the site; both work for one hour, and another message can be requested at most once in five minutes. An account still unconfirmed five days after its first registration is deleted.

### Routing and place search

| Variable | Default | Description |
|---|---|---|
| `TRIPVAULT_ROUTING_PROVIDER` | `ors` | `ors` (openrouteservice, hosted, needs a key), `osrm` or `valhalla` (a server of your own). |
| `TRIPVAULT_ROUTING_BASE_URL` | hosted service | The API root. Required for `osrm` and `valhalla`, such as `http://osrm:5000`. |
| `TRIPVAULT_ROUTING_API_KEY` | | The key for `ors`. Without it road legs are straight-line estimates, marked as such in the interface. |
| `TRIPVAULT_GEOCODING_PROVIDER` | `pelias` | `pelias` (hosted, same kind of key as openrouteservice), `photon` or `nominatim`. |
| `TRIPVAULT_GEOCODING_BASE_URL` | hosted service | The API root. Required for `photon`, and for `nominatim` unless you use the public one. |
| `TRIPVAULT_GEOCODING_API_KEY` | | The key for `pelias`. Without it only searching is off: a place is still added by coordinates, a map link or a click on the map. |

A free openrouteservice key is available at [account.heigit.org](https://account.heigit.org); the same key works for both.

The service limits its own requests by the address it calls: under the openrouteservice and Pelias standard plans for the hosted services, and under one request a second for the OSRM demo server and the public Nominatim, as their usage policies ask. A server of your own is not limited. When the daily limit is reached, new legs become estimates until the next day. Calculated routes and search results are cached and shared by every trip.

Trains, metros and ferries are drawn as straight lines with an estimated time whatever the provider: none of them routes over rails or water.

### Map

| Variable | Default | Description |
|---|---|---|
| `TRIPVAULT_MAP_TILE_URL` | OpenStreetMap | Raster tiles as a template with `{z}`, `{x}` and `{y}`. |
| `TRIPVAULT_MAP_ATTRIBUTION` | OpenStreetMap | The credit the provider asks for. It may contain links and is sanitised before display. |
| `TRIPVAULT_MAP_TILE_ORIGIN` | | The origin of a tile server on plain `http`, such as `http://tiles.lan:8080`. The interface loads tiles only over `https` otherwise. |

The browser loads the tiles for its maps, and the backend fetches the same tiles to draw the maps of a report's PDF, so both must be able to reach the address. The backend keeps each tile for 30 days in the database, outside the backups. A tile server that does not answer costs the PDF's maps their background, never the document.

The OpenStreetMap servers allow light use only and refuse some networks. A refused tile arrives as a picture reading "access blocked" with a `200` status, so look at the map rather than at the status codes. Free alternatives without a key:

```sh
TRIPVAULT_MAP_TILE_URL=https://tile.openstreetmap.de/{z}/{x}/{y}.png   # the same map
TRIPVAULT_MAP_TILE_URL=https://tile.opentopomap.org/{z}/{x}/{y}.png    # contours
```

Change `TRIPVAULT_MAP_ATTRIBUTION` along with the address.

### Media

| Variable | Default | Description |
|---|---|---|
| `TRIPVAULT_MEDIA_MAX_SIZE_MB` | `25` | The largest photograph one upload may be. |
| `TRIPVAULT_MEDIA_TRIP_QUOTA_MB` | `2048` | The initial allowance for one trip on an instance without stored settings. Administrators change it later on the service status page. `0` means no per-trip limit. |
| `TRIPVAULT_STORAGE_QUOTA_MB` | `0` | Original media, avatars, idea photos, attachments and imported track files across the instance. `0` means no application limit. |

Photographs are accepted as JPEG, PNG or WebP, decided from their bytes. A GPX or KML track and an attachment of a place are limited to 10 MB each, and both count towards the trip allowance. Attachments are documents and pictures; archives, programs, scripts, video, sound and office documents with macros are refused.

The instance quota counts the user files Tripvault controls. Generated trip previews, routing, geocoding and map caches, PostgreSQL indexes and WAL, temporary restore data and backup archives are outside it. It is an admission limit rather than a filesystem quota: put the database, media and backups on filesystems or volumes with enough separate capacity, and use host-level quotas when a hard physical boundary is required. Restoring a backup is allowed to exceed the configured application quota so recovery is never stopped halfway; further uploads are refused until usage falls below it or the operator raises the limit.

### Backups

| Variable | Default | Description |
|---|---|---|
| `TRIPVAULT_SECRETS_KEY` | made on first start | Seals the credentials entered in the backup settings. Leave it empty and the backend keeps a key in the `tripvault-config` volume. See [Backups](backups.md#the-secrets-key). |

Destinations, schedules and passphrases are set in the interface, not here.

### Published address

| Variable | Default | Description |
|---|---|---|
| `TRIPVAULT_BIND_ADDRESS` | `127.0.0.1` | The host address the web port is published on. `0.0.0.0` exposes it to the network without TLS. |
| `TRIPVAULT_HTTP_PORT` | `8080` | The host port. |
| `TRIPVAULT_TRUSTED_PROXY` | `127.0.0.1/32` | The address or CIDR of the reverse proxy in front, whose `X-Forwarded-For` names the real client. See below. |

### Images, database, sessions and logs

| Variable | Default | Description |
|---|---|---|
| `TRIPVAULT_VERSION` | `latest` | The image tag. Pin a release to control upgrades. |
| `POSTGRES_DB` | `tripvault` | The database name. |
| `POSTGRES_USER` | `tripvault` | The database user. |
| `TRIPVAULT_AUTH_REFRESH_TOKEN_TTL` | `720h` | How long somebody stays signed in without signing in again. |
| `TRIPVAULT_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. Logs are logfmt on stdout. |

### Tuning

The backend reads a few more variables that the compose file deliberately does not pass, because their defaults suit every ordinary deployment. Setting one in `.env` has no effect on its own; add it to the backend's `environment` in a `docker-compose.override.yaml` beside the compose file:

```yaml
services:
  tripvault-backend:
    environment:
      TRIPVAULT_ROUTING_DAILY_LIMIT: "20000"
```

| Variable | Default | Description |
|---|---|---|
| `TRIPVAULT_ROUTING_REQUESTS_PER_MINUTE`, `TRIPVAULT_ROUTING_DAILY_LIMIT` | from the address | Override the routing limits, for a paid plan. `0` means no limit. |
| `TRIPVAULT_GEOCODING_REQUESTS_PER_MINUTE`, `TRIPVAULT_GEOCODING_DAILY_LIMIT` | from the address | The same for place search. |
| `TRIPVAULT_ROUTING_CACHE_TTL` | `2160h` | How long a calculated route is reused. |
| `TRIPVAULT_GEOCODING_CACHE_TTL` | `720h` | How long a search result is reused. |
| `TRIPVAULT_MEDIA_TRACK_MAX_SIZE_MB` | `10` | The largest GPX or KML file. |
| `TRIPVAULT_MEDIA_ATTACHMENT_MAX_SIZE_MB` | `10` | The largest attachment of a place. |
| `TRIPVAULT_AUTH_ACCESS_TOKEN_TTL` | `15m` | How long an access token lasts before it is refreshed. |
| `TRIPVAULT_DB_MAX_CONNS`, `TRIPVAULT_DB_MIN_CONNS` | `10`, `1` | The database connection pool. |
| `TRIPVAULT_HTTP_READ_TIMEOUT`, `TRIPVAULT_HTTP_WRITE_TIMEOUT`, `TRIPVAULT_HTTP_IDLE_TIMEOUT`, `TRIPVAULT_HTTP_SHUTDOWN_TIMEOUT` | `15s`, `60s`, `120s`, `20s` | HTTP server timeouts. |

## Colour themes

An administrator can add colour themes for everybody on the instance under Administration → Themes. No setting is involved: themes live in the database and travel in every backup. A theme is a JSON file with a light palette, a dark one or both; the page offers a template to start from and previews every theme. The file format and what each colour is for are described in [themes.md](themes.md).

## Reverse proxy and TLS

The web port is published on `127.0.0.1` so that nothing reaches Tripvault except through a proxy that terminates TLS: a reverse proxy on the host, or a tunnel such as Cloudflare Tunnel. The proxy must:

- forward to `http://127.0.0.1:8080` (or your `TRIPVAULT_HTTP_PORT`);
- pass the original `Host`, and set `X-Forwarded-For` and `X-Forwarded-Proto`. The download cookies are marked `Secure` only when the request arrived over `https`;
- allow request bodies as large as `TRIPVAULT_MEDIA_MAX_SIZE_MB` times the files uploaded at once, or simply not limit them, since the backend applies its own limits;
- allow responses to take several minutes: a report's PDF with its maps and photographs can take up to five.

### The trusted proxy

Sign-in attempts are counted per client address. Behind a proxy every request arrives from the proxy, so until the proxy is trusted all visitors count as one client, and one person mistyping a password slows everybody down.

`TRIPVAULT_TRUSTED_PROXY` names the proxy as the frontend container sees it. A proxy or `cloudflared` running on the Docker host reaches the container from the gateway of the Docker network, which `172.16.0.0/12` covers for the default networks:

```sh
TRIPVAULT_TRUSTED_PROXY=172.16.0.0/12
```

The client is then read from `X-Forwarded-For`, skipping trusted hops from the right, so an address a client wrote there itself is never believed.

### Caddy

Caddy obtains a certificate and sets the forwarding headers on its own:

```caddyfile
trips.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

### nginx

```nginx
server {
    listen 443 ssl;
    http2 on;
    server_name trips.example.com;

    ssl_certificate     /etc/letsencrypt/live/trips.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/trips.example.com/privkey.pem;

    client_max_body_size 0;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host              $host;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_request_buffering off;
        proxy_read_timeout 15m;
        proxy_send_timeout 15m;
    }
}
```

## Volumes

| Volume | Mounted at | Holds |
|---|---|---|
| `tripvault-pgdata` | the database's data directory | Every record: accounts, trips, documents, tracks and attachments. |
| `tripvault-media` | `/app/media` | Photographs, avatars and photos of ideas, with their previews under `.previews/`. |
| `tripvault-backups` | `/app/backups` | Archives written to the server's own disk. |
| `tripvault-config` | `/app/config` | The key that seals the stored backup credentials, unless `TRIPVAULT_SECRETS_KEY` is set. |

`docker compose down` keeps the volumes; only `docker compose down --volumes` destroys them. A copy of the volumes is not a backup to rely on: use the [backups](backups.md), which are consistent and verified on restore.

## Health and status

- `GET /healthz` answers while the backend runs; `GET /readyz` also checks the database. Both containers have Docker health checks.
- `GET /version` names the running version.
- Administration → Service status shows the version, the schema version, the accounts, storage use, mail configuration and queue state, the routing and geocoding requests of the last 24 hours with their caches, and how the previews are getting on.

## Security notes

- Only the frontend container publishes a port, and only on localhost by default. The database and the API are reachable from the other containers alone.
- Registration is closed unless an administrator allows self-registration, which needs working mail: such an account cannot sign in until its address is confirmed and is deleted after five days without confirmation. Registration answers known and unknown addresses alike. Otherwise accounts are created by an administrator, through a single-use administrator invitation, or while accepting a trip invitation. Passwords are hashed with argon2id, and an account created with a temporary password must change it at the first sign-in.
- Password recovery, email confirmation and invitation credentials are random, expire, and are consumed once; their credential records store only hashes. A pending outbox message necessarily contains the link it must deliver, so Tripvault erases its recipient and bodies after delivery, permanent failure or expiry, and never sends an expired message. Browser addresses keep the credential in the URL fragment so it does not reach the server, proxy or request log; the interface removes it from the address immediately and sends it only in the API request body. Password recovery gives the same response for known and unknown addresses.
- Sign-in attempts are limited per account and client, per account across clients it has not signed in from, and per client across accounts. Past a limit the answer is `429` with `Retry-After`.
- A session is a short-lived access token and a rotating refresh token. A refresh token used twice ends its session. An administrator can deactivate an account, which ends its sessions at once, and everybody can end their own sessions from the profile.
- Every file is served by the API after checking the reader's access to its trip, never from a static path, and a picture is revalidated before every use so it does not outlive that access.
- A read-only link is a 256-bit token that grants reading one trip and nothing else. It travels in a request header, sits in the fragment of the address the interface builds, and never reaches a log. It can expire, be revoked, and include or leave out private photographs and downloads. What belongs to the travellers - booking references, contacts, who paid, attachments - is never shown through a link.
- The request log records the method, the path and the status only: no headers, no query strings, no tokens.
