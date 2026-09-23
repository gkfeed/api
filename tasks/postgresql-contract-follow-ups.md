# PostgreSQL contract follow-ups for PR 10

Status: implemented and locally verified against PostgreSQL 17 with the
infra-owned schema and application grants. Production readiness remains an
external merge and deployment gate.

## Goal

Align the API storage seam with the infra-owned PostgreSQL contract. The API
must consume the schema and must not run schema or legacy data migrations.

## Nullable passwords

- [x] Represent `users.hashed_password` with a nullable Go type.
- [x] Update user lookups by ID and name so PostgreSQL `NULL` scans without an
      error. Use explicit column lists instead of `SELECT *`.
- [x] Make Basic Auth return `401 Unauthorized` for a user without a password.
- [x] Keep the runtime Argon2id comparison code used by password login.
- [x] Add lookup and Basic Auth tests for a null password, plus a WebAuthn
      lookup test for a passwordless user.

## Feed creation

- [x] Treat `(user_id, url, type)` as the feed identity enforced by the
      database contract.
- [x] Make repeated creation idempotent: return the existing feed with
      `created: false` instead of returning `500` or creating another row.
- [x] Preserve separate feeds when the URL matches but the user or type differs.
- [x] Handle PostgreSQL unique conflicts without relying on a prior read for
      correctness under concurrent requests.
- [x] Extend the storage contract and handler tests with exact-repeat,
      different-type, different-user, and concurrent-repeat cases.

## Remove application migrations

- [x] Remove the startup password migration and its migration-only tests.
- [x] Remove API startup DDL. Replace it with a read-only check for the minimum
      required infra migration when PostgreSQL storage is connected.
- [x] Update route tests so they insert Argon2id hashes directly instead of
      depending on startup conversion.
- [x] Update README documentation to state that the importer owns legacy
      password conversion and infra owns all DDL.
- [x] Do not remove the password hashing package or normal password
      verification used during login.

## External readiness

- [x] Do not deploy this as an intermediate SQLite release.
- [x] Require the infra schema, grants, importer, sequence synchronization, and
      cutover checks before merging or deploying the PostgreSQL storage change.
- [x] Keep user deletion out of scope. The current contract rejects deleting a
      user while dependent feeds, credentials, or tokens exist.

## Definition of done

The API accepts nullable passwords, repeated feed creation is idempotent, no
startup path performs DDL or legacy data conversion, and tests cover the new
contract without requiring a production cutover.


## Validation

The PostgreSQL adapter uses pgx and the infra schema from
`gkfeed/infra/contracts/api.md`. Startup reads `public.schema_migrations`
and requires canonical schema `20260904184133` and application grants
`20260905082946`. Infra's pinned dbmate applied these migrations to a
disposable PostgreSQL 17 instance for testing.

Contract tests run under `gkfeed_api`, including concurrent creation over
multiple connections, null password lookups, Basic Auth, WebAuthn credential
lookup, and refresh token persistence. Local handler tests cover both feed
creation endpoints. Read-only schema checks test missing and later migrations.

Validation passed with `go test -race ./...`, `go vet ./...`, and a
`CGO_ENABLED=0` API build. A binary smoke test verified startup under
`gkfeed_api`, passwordless rejection, Argon2id login, repeat feed creation,
and startup rejection without the migration registry. Deployment now requires
a manual workflow run with `cutover_ready` confirmed.

External readiness requirements above are documented gates, not verified
production infrastructure or cutover results. Nothing was merged or deployed.
