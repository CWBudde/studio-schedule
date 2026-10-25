# Operations

How to run, update and release Studio Schedule. The app runs as **one instance**: rate limits and
background jobs live in the process.

## Container image

The image `ghcr.io/cwbudde/studio-schedule` holds the single binary with the web UI embedded, on
Alpine Linux, for `linux/amd64` and `linux/arm64`. The workflow `.github/workflows/image.yaml`
publishes it:

| Tag      | From                                                   |
| -------- | ------------------------------------------------------ |
| `edge`   | a push to `main`, once every job of `test.yaml` passed |
| `vX.Y.Z` | a published release (also `X.Y` and `latest`)          |

`test.yaml` calls `image.yaml` for a push to `main` after its tests succeeded, so a failing commit
never becomes `edge`. A push that changes only `docs/` and Markdown files since the last successful
run on `main` builds no image.

What the image sets up:

- It runs as the non-root user `app` (UID and GID 10001) and listens on port 8080.
- The entrypoint is the `studio-schedule` binary and the default command is `serve`, so
  `docker run … ghcr.io/cwbudde/studio-schedule:edge migrate up` runs another subcommand instead.
- `LOG_FORMAT=json` is the default (override with `LOG_FORMAT=text`); `LOG_LEVEL` defaults to
  `info`.
- `XDG_CACHE_HOME=/var/cache/studio-schedule` holds the compiled receipt renderer (wazero's
  compilation cache, about 60 MB), so a restart doesn't compile it again. The image declares no
  volume, which would leave an anonymous volume behind for every container. Mount a named volume
  there to keep the cache when the container is replaced, e.g.
  `docker run -v studio-schedule-cache:/var/cache/studio-schedule …` (Compose does this). It is a
  cache: deleting it is safe.
- The health check runs `studio-schedule healthcheck`, which requests `/healthz` at the port of
  `HTTP_ADDR` and fails unless the answer is 200 within three seconds. Set the listen address with
  `HTTP_ADDR`, not with `serve --addr`, so that the health check finds the server.
- `ca-certificates` (SMTP over TLS, PostgreSQL with `sslmode=verify-full`) and `tzdata` are
  installed.

Trivy scans every pushed image, the `linux/amd64` and `linux/arm64` variants separately (code
scanning categories `trivy-image-amd64` and `trivy-image-arm64`). Its findings show up as code
scanning alerts on GitHub and never fail the build.

### Building the image

The receipt renderer `internal/pdf/typstwasm/receipt.wasm` is a Git LFS file. Pull it before you
build, otherwise the build stops with a message that says so:

```sh
git lfs pull
docker build --build-arg VERSION="$(git describe --tags --always --dirty)" -t studio-schedule:local .
```

For both architectures: `docker buildx build --platform linux/amd64,linux/arm64 .`. The web and Go
stages run on the build machine and cross-compile, so only the small runtime stage is emulated.

### Docker Compose

`docker-compose.yml` runs the app with PostgreSQL 18 (data in the volume `postgres`) and
[Mailpit](https://mailpit.axllent.org), which catches every mail the app sends and shows it at
http://localhost:8025. It suits a local trial and is a starting point for a single-server
deployment.

```sh
cp .env.example .env                    # set POSTGRES_PASSWORD (and BASE_URL)
docker compose up -d --build            # the app restarts until the schema is migrated
docker compose run --rm app migrate up
docker compose run --rm app init        # set up the school and the first admin
# or: docker compose run --rm app seed  # the fictional demo data instead
```

Migrations never run on start. `serve` refuses to start while the schema doesn't match the binary,
and the container restarts until `migrate up` has run. Run `migrate up` after every image update
as well.

Compose builds `DATABASE_URL` from `POSTGRES_USER`, `POSTGRES_PASSWORD` and `POSTGRES_DB`.
`APP_PORT` and `MAILPIT_PORT` change the host ports; `BASE_URL` defaults to
`http://localhost:<APP_PORT>`. The compiled receipt renderer lives in the volume `cache`.

While `SMTP_HOST` is empty, the app sends every mail to Mailpit (port 1025, no TLS, from
`schedule@example.test` unless `SMTP_FROM` is set). For a real mail server set `SMTP_HOST` and
`SMTP_FROM` in `.env`. The `SMTP_*` values then apply as given: an empty `SMTP_TLS` or `SMTP_PORT`
means the app's default, STARTTLS on port 587.

Compose builds the image from the checkout. To run a published image instead, set
`STUDIO_SCHEDULE_IMAGE=ghcr.io/cwbudde/studio-schedule:edge` (or a release tag) in `.env`, then:

```sh
docker compose pull app
docker compose up -d --no-build
```

## Upgrading PostgreSQL to a new major version

Renovate updates the PostgreSQL image within its major version only. A new major version can't
read the old one's data files, so the data moves with a dump and a restore. The PostgreSQL images
from version 18 on keep each major version's data in its own directory of the volume
(`/var/lib/postgresql/<major>/docker`): the new version starts with an empty cluster, and the old
data stays untouched until you remove it. With Compose, from 18 to 19 for example:

```sh
docker compose stop app
# A dump of the app's database (it holds personal data: keep it safe, delete it afterwards).
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom' > studio.dump
# In docker-compose.yml: image: postgres:19-alpine
docker compose up -d postgres
docker compose exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --exit-on-error' < studio.dump
docker compose run --rm app migrate status   # every migration applied, none pending
docker compose up -d
```

If something fails, set the old image tag again: the old version still finds its data. Once the
app runs on the new version, remove the old data with
`docker compose exec postgres rm -rf /var/lib/postgresql/18` and delete `studio.dump`.

## Releases

[release-please](https://github.com/googleapis/release-please) maintains a release PR from the
Conventional Commits on `main`. Merging that PR tags `vX.Y.Z` and publishes the GitHub release.
The release then triggers `image.yaml` (image tags `vX.Y.Z`, `X.Y` and `latest`) and `pages.yaml`
(the demo).

GitHub doesn't start workflows for events caused by the default `GITHUB_TOKEN`. With that token, the
release PR gets no CI run and the published release builds no image. release-please therefore uses
the repository secret `RELEASE_PLEASE_TOKEN` when it exists, a fine-grained personal access token:

1. GitHub → Settings → Developer settings → Personal access tokens → Fine-grained tokens →
   **Generate new token**.
2. **Resource owner:** the owner of the repository. **Repository access:** _Only select
   repositories_ → `studio-schedule`.
3. **Repository permissions:** _Contents_ read and write, and _Pull requests_ read and write
   (_Metadata_ read is added automatically). _Workflows_ isn't needed: release PRs only change
   `CHANGELOG.md`, `version.txt` and `.release-please-manifest.json`.
4. Choose an expiry (at most one year) and note when to renew it. Once it expires, release-please
   falls back to `GITHUB_TOKEN` and releases build no image again.
5. Repository → Settings → Secrets and variables → Actions → **New repository secret**, named
   `RELEASE_PLEASE_TOKEN`, with the token as its value.

The first push creates the package on ghcr.io. Its visibility starts as private: to let others
pull it without signing in, set it to public under the package's settings.
