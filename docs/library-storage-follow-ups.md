# Library storage follow-ups

Historical plan for the SQLite storage seam. PostgreSQL runtime configuration,
migration ownership, and deployment requirements now follow [README](../README.md)
and [the PostgreSQL follow-ups](../tasks/postgresql-contract-follow-ups.md).

## Remove the deprecated hide route

Remove `POST /api/v1/add_deleted_items` after client telemetry or an explicit
consumer audit confirms that no supported client still calls it. Until then it
must remain an authenticated, non-mutating `410 Gone` compatibility stub.

No removal date is assigned yet.

## Auth repository injection

Implemented. `cmd/api` constructs separate user, WebAuthn credential,
refresh-token and browser-session repositories from its owned SQL pool.
Password authentication, WebAuthn and HTTP handlers receive the narrow
interfaces they use through constructors. `internal/db` has no package-level
pool configuration; tests inject their own repositories or local lookup fakes.

Password and passkey handlers in one router share the same `SessionStore` and
its lifecycle lock. Refresh mutations retain the PostgreSQL transaction advisory
lock and use INSERT/DELETE without requiring UPDATE privileges. The lifecycle lock remains held through
commit and access-session publication or revocation, including cancellation
after a successful commit. Browser-session activity and revocation retain their
existing transaction boundaries. The composition root closes the pool after
HTTP shutdown; repositories never close it.

The v1/v2 HTTP contracts, token formats, expiry rules and infra-owned schema
remain unchanged. Regression tests run two live handlers with separate databases
and overlapping user IDs, and verify isolation of password/Basic authentication,
credentials, refresh tokens and browser sessions. The passkey login test also
keeps a second application alive while completing the first application's
ceremony. Local auth and SQL tests can run in parallel without swapping a global
pool or function.
