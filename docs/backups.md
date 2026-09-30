# Backups and restore

A Tripvault backup is a copy of the whole instance: every account, trip, document, track, attachment and photograph. A restore puts the whole instance back from one archive. There is no export of a single trip.

Backups are set up and run by an administrator under Administration → Backups. Archives never pass through the browser: they are written to a destination and read back from it by the server.

## What an archive holds

An archive is a `tar.gz` named `tripvault_<UTC time>.tar.gz`, or `tripvault_<UTC time>.tar.gz.age` when it is encrypted. It contains:

- a manifest with the archive format, the product version and the schema version that wrote it;
- a dump of every table, each with its checksum;
- every stored file - photographs, avatars, photos of ideas - each with its checksum.

It leaves out what the service can make again: the previews of photographs, which are rendered anew after a restore, and the cache of map tiles.

## Destinations

Each backup configuration writes to one destination. Keep at least two: one on the server's own disk, and one on another machine, because a copy kept only beside the database dies with the disk that holds it.

### This server's disk

Archives go to the `tripvault-backups` volume, mounted at `/app/backups` in the backend container. The volume can be bound to a directory of the host, such as a mounted network share, with a `docker-compose.override.yaml`:

```yaml
services:
  tripvault-backend:
    volumes:
      - /mnt/nas/tripvault:/app/backups
```

The backend runs as user `10001`, which must be able to write there.

If the directory cannot be opened when the backend starts, the instance starts without backups and the Backups screen says so, rather than refusing to start.

### SFTP

An SFTP destination takes a host, a port (22 by default), a user and a directory, and signs in with a password or a private key, optionally with its own passphrase.

The server's host key protects against a machine pretending to be your backup server. Paste the key in the form, as `ssh-keyscan` prints it:

```sh
ssh-keyscan -t ed25519 backup.example.com
```

Left empty, the key the server presents at the first connection is accepted and pinned from then on; a later connection presenting another key fails. Pasting it yourself is safer, because it does not trust the first connection.

**Check** in the form connects, verifies the host key, opens the directory, and writes and removes a small file, reporting each step on its own, so a wrong password is told apart from a directory the account may not write to.

## Schedule and retention

- **Schedule** is a five-field cron expression, such as `0 3 * * *` for every night at three, or a descriptor such as `@daily`. Without one, the configuration runs only when somebody presses **Run now**.
- **Retention** keeps either the newest N archives or the archives of the last N days; with neither, every archive is kept. Retention removes only archives this configuration wrote, as its own history records them, so two instances writing to one directory do not remove each other's.

The configuration's **History** lists every run with its outcome. **Stop backing up** withdraws a configuration and leaves the archives it wrote where they are, still listed for a restore.

## Encryption

A configuration may lock its archives with a passphrase. They are encrypted with [age](https://age-encryption.org) using that passphrase (scrypt with ChaCha20-Poly1305), so they can also be opened without Tripvault:

```sh
age -d -o tripvault.tar.gz tripvault_20260930T031500.000Z.tar.gz.age
```

Keep the passphrase somewhere other than the instance. An encrypted archive cannot be restored without it.

## The secrets key

The SFTP password or private key and the archive passphrase are entered in the interface and stored sealed with the instance's secrets key, so a dump of the database alone opens none of them. Only their names ever leave the server.

The key is never kept in the database, because the archives contain that database. By default the backend makes one on its first start and keeps it in the `tripvault-config` volume. To keep it in your own secret store instead, generate one and set `TRIPVAULT_SECRETS_KEY`:

```sh
docker run --rm ghcr.io/nir0k/tripvault-backend:latest --secrets-key
```

Losing the key loses only the stored credentials: enter the SFTP password or key and the passphrase again. It does not make existing archives unreadable, as long as you still know their passphrase.

## Restore

A restore replaces **everything** on the instance - accounts, sessions, trips and files - with the contents of the archive. It is refused when the archive's schema is newer than the running build, so restore into the same release or a newer one, never an older one.

Before anything is replaced the archive is unpacked and every table and file is verified against its checksum; a damaged archive changes nothing. While the restore runs, requests in flight are cancelled and new ones are answered with `503`. Backups and restores do not run at the same time.

Sessions are restored with everything else, so the administrator who started the restore is signed out unless their account and session are in the archive. Sign in again with an account the archive holds.

### From the interface

Under Administration → Backups, **Restore** asks where the archive is - this server's disk or an SFTP configuration - and lists the archives there. The listing is read from the destination itself, so a fresh instance sees archives written by an earlier one. An encrypted archive uses the configuration's own passphrase, or the one you type for an archive another instance wrote. Type the confirmation word, and the page follows the restore through verifying, preparing and applying until it is done.

To restore an archive that is on neither destination, copy it into the backups volume first. The name must start with `tripvault_`, and the file must be readable by the backend:

```sh
docker compose cp tripvault_20260930T031500.000Z.tar.gz tripvault-backend:/app/backups/
```

### From the command line

When there is no working interface - a new server after losing the old one - restore before the service starts. The backend binary restores the archive into the configured database and media volume and exits:

```sh
docker compose stop tripvault-frontend tripvault-backend

docker compose run --rm \
  -v "$PWD/tripvault_20260930T031500.000Z.tar.gz:/restore/archive.tar.gz:ro" \
  tripvault-backend --restore /restore/archive.tar.gz

docker compose up -d
```

For an encrypted archive, put the passphrase in a file and pass it too:

```sh
docker compose run --rm \
  -v "$PWD/tripvault_20260930T031500.000Z.tar.gz.age:/restore/archive.tar.gz.age:ro" \
  -v "$PWD/passphrase.txt:/restore/passphrase.txt:ro" \
  tripvault-backend --restore /restore/archive.tar.gz.age \
  --restore-passphrase-file /restore/passphrase.txt
```

The files must be readable by user `10001` inside the container. The new instance needs its own `.env`; its `TRIPVAULT_AUTH_JWT_SECRET` may differ from the old one, which only means everybody signs in again.
