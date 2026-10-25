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
`20260905082946_add_application_roles`, and the item sync migration
`20260927071753_add_item_changes`. Missing migrations or an unreadable
registry prevent startup. Later migrations are allowed. The API also requires
the `browser_sessions` schema and application grants described in the
[browser session contract](docs/browser-sessions.md). Infra must apply that
migration before deployment; the API does not execute its DDL.

The importer owns legacy password conversion and deleted-item tombstone
processing. The API never creates tables or migrates legacy data. SQLite is
used only for local test fixtures.

```sh
make dev
```

The API listens on <http://localhost:8086>. Browser login uses persistent cookie sessions through `/api/v2/sessions`. The v1 password login returns a short-lived opaque access token and a rotating refresh token. Access tokens are sent as `Authorization: Bearer <token>`; Basic authentication remains available for existing v1 clients. The `hashed_password` column stores PHC-formatted Argon2id hashes; the importer converts legacy plaintext passwords before cutover. Null passwords are valid for passwordless users; Basic Auth rejects them.

Run the local quality checks with:

```sh
make check
```

`make lint` runs `go vet` and the same `gopls` diagnostics used by Go editor
integrations. Install `gopls` first with `make install-lint`.

### Swagger 2.0 workflow

Edit the Swaggo annotations in Go handlers and their DTOs. Generated
`app/cmd/api/docs/{docs.go,swagger.json,swagger.yaml}` are ignored by Git.
Do not edit or commit them. `app/generate-swagger.sh` pins the generator version.

`make dev`, `make build`, `make test`, `make vet`, `make lint`, `make format`
and `make check` generate fresh docs first. On a clean checkout, prepare them
before using direct Go commands or opening the project in a Go editor:

```sh
make swagger
cd app && go test ./...
```

`make swagger-check` generates and validates Swagger 2.0. `make check` also
runs the existing Swagger/HTTP tests and vet. The first run downloads the
pinned tools. `make build` writes `.cache/bin/gkfeed-api`.

CI runs on PRs targeting `v1` and pushes to `v1`, and publishes JSON/YAML
in a `swagger-<tested commit SHA>` Actions artifact. Consumers of the former
raw GitHub paths should use that artifact or `/api/swagger/doc.json`.
Swagger UI remains at `/api/swagger/index.html`.

Docker generates Swagger in its build stage and embeds it in the binary.
Keep HTTP tests for contract changes; text-only documentation edits do not
need a new endpoint-specific Swagger test.

## Configuration

Configuration is read from environment variables at startup:

| Variable | Default | Description |
| --- | --- | --- |
| `GKFEED_ADDRESS` | `:8086` | HTTP server listen address |
| `GKFEED_DATABASE_URL` | none (required) | PostgreSQL application connection URL |
| `GKFEED_ALLOWED_ORIGINS` | Localhost development origins | Comma-separated exact origins for CORS and browser-session mutation checks |
| `GKFEED_SYNC_CURSOR_SECRET` | Random process-local key | Sync cursor key; accepts `GKFEED_JWT_SECRET` as fallback |
| `GKFEED_ACCESS_TTL` | `30m` | Access-token lifetime |
| `GKFEED_JWT_REFRESH_TTL` | `2160h` | Refresh-token idle lifetime |
| `OPENROUTER_API_KEY` | none | Enables Jev feed type detection for URL-only v2 creation. Explicit `type` or `types` works without it. |

## Docker

```sh
docker compose up --build -d
```

Docker Compose requires `GKFEED_DATABASE_URL` for an externally provisioned
PostgreSQL database. It does not start a database or run migrations.

Set CORS/Origin, sync secret, WebAuthn and token TTL settings in the shell or a
local `.env` file. Compose forwards them with empty defaults, preserving the
API defaults and `GKFEED_JWT_SECRET` fallback. For a hosted frontend, configure
its allowed origin and WebAuthn RP. Set a stable sync secret to preserve cursors
across restarts.

Check Compose configuration forwarding with synthetic values, without starting
containers or connecting to PostgreSQL:

```sh
env -u GKFEED_TEST_DATABASE_URL python3 tests/test_compose_config.py
```

The deployment workflow runs tests on pushes to `master`, but deployment
requires a manual run with `cutover_ready` confirmed. Confirm that the infra
schema, grants, importer, sequence synchronization, and cutover checks are
complete before selecting that input.

## API routes

Swagger UI is available at `/api/swagger/index.html`.

The resource routes are available under `/api/v2`. Browser sessions, Basic and
Bearer credentials are accepted for every route below. Feed creation accepts one `url` with
optional `title` and either `type` or `types`. Deleting a feed also deletes its items.

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/api/v2/feeds` | List the user's feeds |
| `GET` | `/api/v2/feeds/{id}` | Get an owned feed; missing or foreign IDs return `404` |
| `POST` | `/api/v2/feeds` | Resolve a source and create selected subscriptions atomically |
| `DELETE` | `/api/v2/feeds/{id}` | Delete a feed and its items |
| `GET` | `/api/v2/items?limit=<n>&cursor=<id>` | List items with the existing cursor pagination |
| `GET` | `/api/v2/items/sync` | Start or continue an item sync with an opaque cursor |
| `GET` | `/api/v2/items/changes` | Read item changes after a sync cursor |
| `GET` | `/api/v2/items/{id}` | Get an owned item and its feed |
| `DELETE` | `/api/v2/items/{id}` | Delete an owned item |

Successful v2 list, read and delete responses keep their v1 JSON shapes and status codes.
`GET /api/v2/feeds/{id}` returns a feed object from the list. The v1 routes
below remain available while clients migrate. v2 has no separate lazy creation
route or item PATCH route.

### Unified v2 feed creation

`POST /api/v2/feeds` accepts one absolute HTTP(S) URL without credentials.
The URL is required; other fields are optional:

```json
{"url":"https://www.instagram.com/example","types":["inst","stories"],"title":"My source"}
```

- `type` selects exactly one subscription. For example, `"type":"inst"`
  creates posts only, while `"type":"stories"` creates stories only.
- `types` selects a nonempty array of distinct supported types. It cannot be
  combined with a nonempty `type`. Results follow the selected order.
- If neither is supplied, the API calls OpenRouter Decisions directly using
  `typesafe/jev-1.13`, passing the URL and optional supplied title. The response
  must name a type from the API catalog and have confidence at least `0.35`.
  An automatically detected Instagram profile creates posts and stories.
- The catalog returned by `GET /api/v1/feed_types` also supplies validation and
  Jev's choice criteria. It is maintained in the API alongside parser capability
  changes. The frontend's additional `author.today` type is not yet in this API
  catalog; this change does not expand parser support.
- An explicit title or supported type takes precedence over metadata. A manual
  type is preserved even if it differs from the apparent source. Instagram
  titles default to the profile name; other titles use Open Graph or page
  titles, then a name derived from the URL if metadata is unavailable.
- URL input is limited to 8192 bytes and titles to 1000 bytes. Surrounding
  whitespace is removed from URL, title and the singular type. Invalid URLs,
  unknown/repeated types and an empty `types` selection return `400`.

The API normalizes recognized YouTube channel pages to
`https://www.youtube.com/channel/<Channel ID>` and preserves the chosen tab.
Channel roots, `/videos`, `/shorts` and `/streams` remain distinct subscriptions.
Handles and `/c/` or `/user/` aliases require a verified ID from channel-page
metadata. Conflicting or missing identity evidence fails creation. Permanent
Channel-ID URLs need no identity lookup; video and playlist URLs are preserved.
Instagram profile URLs normalize to HTTPS, `www.instagram.com` and a lowercase
username, without query, fragment or trailing slash. Other URLs retain their
path, query and fragment; host casing and default ports are normalized.
The existing `tok.adminforge.de/@...` to `www.tiktok.com/@...` mirror conversion
is also retained. Existing aliases and old records are not migrated or merged.

The response is always `200` with a `results` array, including for one feed:

```json
{"results":[{"created":true,"item":{"id":123,"title":"My source","type":"inst","url":"https://www.instagram.com/example","userid":1}},{"created":false,"item":{"id":124,"title":"Existing name","type":"stories","url":"https://www.instagram.com/example","userid":1}}]}
```

Metadata resolution completes before the database transaction. All missing
subscriptions are inserted in one transaction. A failure rolls back every new
feed from that request; existing feeds and their titles stay unchanged.
Uniqueness remains `(user_id, url, type)`. Concurrent creation and retries that
resolve to the same canonical URL and types return the same resources, with
`created:false` for existing ones. A retry resolves missing data again; no
asynchronous operation, request replay cache or `Idempotency-Key` is introduced.

The request has a 15-second budget; each Jev or metadata request has a 5-second
timeout. Required resolution failures create no records:

| Status | Error code | Meaning |
| --- | --- | --- |
| `400` | `invalid_creation` | Invalid URL or selection |
| `422` | `type_required` | Jev confidence below `0.35`; pass an explicit type |
| `502` | `detection_failed` | Jev returned an invalid answer or failed |
| `502` | `youtube_channel_unresolved` | Channel identity could not be verified |
| `503` | `detection_unavailable` | OpenRouter key is missing; explicit types still work |
| `504` | `resolution_timeout` | Required resolution exceeded its deadline |

These errors use `{"error":{"code":"type_required","message":"..."}}`.
Malformed JSON and authentication/storage errors retain existing error formats.
An optional title lookup failure uses the URL fallback. Metadata fetching
allows public HTTP(S) destinations on ports 80/443 only, checks redirects and
resolved addresses, and bounds response size. The OpenRouter key is never sent
to metadata destinations or redirect targets.

This replaces the previous unstable v2 creation request/response contract.
v1 `add` and `add_lazy` retain their input, response and resolution behavior.
Frontend migration to this operation is a separate change. No infra DDL or new
application privileges are required.

### Resource errors

`GET /api/v2/feeds`, `GET` and `DELETE /api/v2/feeds/{id}`, and `GET` and
`DELETE /api/v2/items/{id}` return the errors listed below as `application/json`:

```json
{"error":"not_found","message":"Resource not found"}
```

Clients should use the stable `error` code to distinguish failures:

- `400`, `invalid_id`: the ID must be a positive integer within the server's integer range.
- `401`, `unauthorized`: credentials are missing or invalid. Authentication challenges and cookie headers are preserved.
- `404`, `not_found`: the resource is missing or belongs to another user. Both cases return the same body.
- `405`, `method_not_allowed`: the method is unsupported on the resource path. `Allow` lists the supported methods.
- `500`, `internal_error`: the request failed. Internal details are logged rather than returned.

Authentication is checked before ID validation. Unsupported methods return `405`
before authentication. Successful deletion returns `204` with no body; reading
or deleting the resource again returns `404`. Feed deletion also removes its items.

The error format applies only to these operations and their method errors.
Feed creation, the item list, sync/changes, unknown URLs, and v1 keep their
existing responses. v1 removal remains conditional on front and gkbot migration
and verification of their deployed versions, as described in
[`tasks/remove-v1-library-routes.md`](tasks/remove-v1-library-routes.md).
Cookie authentication's Origin-policy failures retain the browser-session
contract described in [`docs/browser-sessions.md`](docs/browser-sessions.md).

### Legacy v1 routes

| Method | Route | Authentication | Purpose |
| --- | --- | --- | --- |
| `GET` | `/api/v1/list` | Basic, Bearer or cookie | List the user's feeds |
| `GET` | `/api/v1/feed_types` | None | List feed types supported by the parser |
| `GET` | `/api/v1/feed` | Basic, Bearer or cookie | Return the user's RSS feed |
| `POST` | `/api/v1/add` | Basic, Bearer or cookie | Add a feed |
| `POST` | `/api/v1/add_lazy` | Basic, Bearer or cookie | Add a feed inferred from its URL |
| `DELETE` | `/api/v1/delete?id=<id>` | Basic, Bearer or cookie | Permanently delete a feed and all its items |
| `DELETE` | `/api/v1/items/{id}` | Basic, Bearer or cookie | Permanently delete an owned item |
| `POST` | `/api/v1/add_deleted_items` | Basic, Bearer or cookie | Deprecated compatibility stub; returns `410 Gone` |
| `GET` | `/api/v1/get_items` | Basic, Bearer or cookie | Return cursor-paginated items |
| `GET` | `/api/v1/item?id=<id>` | Basic, Bearer or cookie | Return the authenticated user's item and its feed |
| `GET` | `/api/v1/auth/me` | Basic, Bearer or cookie | Return the authenticated user |
| `POST` | `/api/v2/sessions` | Password and allowed Origin | Create a browser session, return user and set cookie |
| `POST` | `/api/v2/sessions/passkey/begin` | Allowed Origin | Begin browser passkey login |
| `POST` | `/api/v2/sessions/passkey/finish` | Passkey assertion and allowed Origin | Create the same browser session |
| `GET` | `/api/v2/me` | Cookie | Restore the current browser user |
| `DELETE` | `/api/v2/sessions/current` | Allowed Origin, cookie if present | Revoke current browser session and clear cookie |
| `POST` | `/api/v1/auth/login` | None | Exchange username and password for access and refresh tokens |
| `POST` | `/api/v1/auth/refresh` | None | Rotate a refresh token and issue a new token pair |
| `POST` | `/api/v1/auth/logout` | Refresh token | Revoke the refresh-token family |
| `POST` | `/api/v1/auth/logout-all` | Basic, Bearer or cookie | Revoke all sessions for the authenticated user |

Browser sessions survive API restarts and browser closure. They expire after 30
days of inactivity, with an absolute limit of 90 days. Unsafe cookie requests
require an allowed `Origin`. See the [browser session contract](docs/browser-sessions.md)
for response details, the PostgreSQL prerequisite, and deferred frontend/dev-proxy work.

Refresh token IDs in the infra-owned `refresh_tokens` table contain only SHA-256 token digests and random family IDs. Consumed tokens remain as expired rows so reuse can be detected. Each refresh rotates the token; reusing an old token revokes the entire token family and its access sessions. Access sessions are held in memory, so restarting the API invalidates access tokens while valid refresh tokens can obtain new ones.

The deprecated `/api/v1/add_deleted_items` endpoint no longer reads its body or
changes data. Authenticated requests receive a JSON `410 Gone` response pointing
clients to `DELETE /api/v1/items/{id}`; unauthenticated requests still receive
`401 Unauthorized`.

### Item synchronization

`GET /api/v2/items/sync?limit=100` returns `items`, `next_cursor`, `has_more`, and
`sync_cursor`. Fetch subsequent pages with `next_cursor` while `has_more` is
true. The first page fixes an upper item ID and change sequence; later inserts
cannot shift earlier pages, and deleted items are skipped. Apply changes from
the first page's `sync_cursor` after finishing the full list to catch mutations
that happened during paging. Cursors are encrypted, tied to the authenticated
user, and invalidated when the sync cursor key rotates. Set
`GKFEED_SYNC_CURSOR_SECRET` to a random value of at least 32 bytes to keep
cursors valid across API restarts and instances. The API also accepts the
existing `GKFEED_JWT_SECRET` as a compatibility fallback for this key. Without
either variable, startup generates a process-local key; browser sessions still
survive restart, but sync cursors must be recreated.

`GET /api/v2/items/changes?cursor=<sync_cursor>&limit=100` returns `upserted`,
`deleted_ids`, `next_cursor`, and `has_more`. Apply each response before using
its `next_cursor`; continue immediately while `has_more` is true, then poll
later with the last cursor. No changes return empty arrays and a valid
next cursor. Repeated mutations of the same item within one response collapse
to its last state. Both routes cap `limit` at 500 and reject invalid or
cross-user cursors with `400`.

The v2 routes require the item change table and triggers from
[`gkfeed/infra` migration `20260927071753`](https://github.com/gkfeed/infra/blob/master/db/migrations/20260927071753_add_item_changes.sql).
Apply that migration before deploying this API version. The triggers record
database writes, including deletes caused by feed deletion. Do not prune the
change table without a cursor expiry and recovery policy.

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
