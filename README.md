# GKFeed API

GKFeed is a small Go HTTP API that stores feed subscriptions and items in PostgreSQL and can expose a user's items as JSON or RSS.

## Requirements

- Go 1.25 or newer
- PostgreSQL provisioned by [gkfeed/infra](https://github.com/gkfeed/infra)
- A login with membership in the infra-owned `gkfeed_api` role
- A C compiler for the SQLite test fixtures

## Development

Set `GKFEED_DATABASE_URL` to your PostgreSQL application connection URL before starting the API.

Do not deploy this branch as an intermediate SQLite release. Before merge or
deployment, confirm the infra schema and grants, importer completion, sequence
synchronization, and cutover checks.

Infra owns all DDL. Startup only reads `public.schema_migrations` and requires
`20260904184133_create_canonical_schema` and
`20260905082946_add_application_roles`. Missing migrations or an unreadable
registry prevent startup. Later migrations are allowed.

The importer owns legacy password conversion and deleted-item tombstone
processing. The API never creates tables or migrates legacy data. SQLite is
used only for local test fixtures.

```sh
make dev
```

The API listens on <http://localhost:8086>. Password login returns a short-lived opaque access token and a rotating refresh token. Access tokens are sent as `Authorization: Bearer <token>`; Basic authentication remains available for existing v1 clients. The `hashed_password` column stores PHC-formatted Argon2id hashes; the importer converts legacy plaintext passwords before cutover. Null passwords are valid for passwordless users; Basic Auth rejects them.

Run the local quality checks with:

```sh
make check
```

`make lint` runs `go vet` and the same `gopls` diagnostics used by Go editor
integrations. Install `gopls` first with `make install-lint`.

## Configuration

Configuration is read from environment variables at startup:

| Variable | Default | Description |
| --- | --- | --- |
| `GKFEED_ADDRESS` | `:8086` | HTTP server listen address |
| `GKFEED_DATABASE_URL` | none (required) | PostgreSQL application connection URL |
| `GKFEED_ALLOWED_ORIGINS` | Localhost development origins | Comma-separated CORS origins |
| `GKFEED_ACCESS_TTL` | `30m` | Access-token lifetime |
| `GKFEED_JWT_REFRESH_TTL` | `2160h` | Refresh-token idle lifetime |

## Docker

```sh
docker compose up --build -d
```

Docker Compose requires `GKFEED_DATABASE_URL` for an externally provisioned
PostgreSQL database. It does not start a database or run migrations.
The deployment workflow runs tests on pushes to `master`, but deployment
requires a manual run with `cutover_ready` confirmed. Confirm that the infra
schema, grants, importer, sequence synchronization, and cutover checks are
complete before selecting that input.

## API routes

Swagger UI is available at `/api/swagger/index.html`.

| Method | Route | Authentication | Purpose |
| --- | --- | --- | --- |
| `GET` | `/api/v1/list` | Basic | List the user's feeds |
| `GET` | `/api/v1/feed_types` | None | List feed types supported by the parser |
| `GET` | `/api/v1/feed` | Basic | Return the user's RSS feed |
| `POST` | `/api/v1/add` | Basic | Add a feed |
| `POST` | `/api/v1/add_lazy` | Basic | Add a feed inferred from its URL |
| `DELETE` | `/api/v1/delete?id=<id>` | Basic or Bearer | Permanently delete a feed and all its items |
| `DELETE` | `/api/v1/items/{id}` | Basic or Bearer | Permanently delete an owned item |
| `POST` | `/api/v1/add_deleted_items` | Basic or Bearer | Deprecated compatibility stub; returns `410 Gone` |
| `GET` | `/api/v1/get_items` | Basic | Return cursor-paginated items |
| `GET` | `/api/v1/item?id=<id>` | Basic or Bearer | Return the authenticated user's item and its feed |
| `GET` | `/api/v1/auth/me` | Basic or Bearer | Return the authenticated user |
| `POST` | `/api/v1/auth/login` | None | Exchange username and password for access and refresh tokens |
| `POST` | `/api/v1/auth/refresh` | None | Rotate a refresh token and issue a new token pair |
| `POST` | `/api/v1/auth/logout` | Refresh token | Revoke the refresh-token family |
| `POST` | `/api/v1/auth/logout-all` | Basic or Bearer | Revoke all sessions for the authenticated user |

Refresh token IDs in the infra-owned `refresh_tokens` table contain only SHA-256 token digests and random family IDs. Consumed tokens remain as expired rows so reuse can be detected. Each refresh rotates the token; reusing an old token revokes the entire token family and its access sessions. Access sessions are held in memory, so restarting the API invalidates access tokens while valid refresh tokens can obtain new ones.

The deprecated `/api/v1/add_deleted_items` endpoint no longer reads its body or
changes data. Authenticated requests receive a JSON `410 Gone` response pointing
clients to `DELETE /api/v1/items/{id}`; unauthenticated requests still receive
`401 Unauthorized`.

Repeated feed creation uses `(user_id, url, type)` as its identity and returns the
existing feed with `created: false`. The original title remains unchanged.
Different users or feed types have separate feeds even when their URLs match.

User deletion is out of scope. The infra contract rejects deleting users with
dependent feeds, credentials, or tokens.


## PostgreSQL contract tests

Provision a disposable PostgreSQL 17 database with infra's pinned
`make migrate` and confirm `make status` reports no pending migrations. Then run:

```sh
GKFEED_TEST_DATABASE_URL='<disposable operator connection URL>' make check
```

This opt-in suite truncates users and dependent application data. Never point
it at a shared or production database. The operator connection seeds fixtures;
repository and auth operations use `SET ROLE gkfeed_api` through pgx connection
parameters. Tests cover concurrent feed creation across multiple connections,
nullable passwords, WebAuthn lookups, refresh tokens, and application grants.
Without this variable, `make check` runs local fixtures and skips PostgreSQL
integration tests.
