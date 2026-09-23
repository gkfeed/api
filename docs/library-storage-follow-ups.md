# Library storage follow-ups

Historical plan for the SQLite storage seam. PostgreSQL runtime configuration,
migration ownership, and deployment requirements now follow [README](../README.md)
and [the PostgreSQL follow-ups](../tasks/postgresql-contract-follow-ups.md).

## Remove the deprecated hide route

Remove `POST /api/v1/add_deleted_items` after client telemetry or an explicit
consumer audit confirms that no supported client still calls it. Until then it
must remain an authenticated, non-mutating `410 Gone` compatibility stub.

No removal date is assigned yet.

## Inject auth repositories

Replace the temporary package-level `internal/db.Configure` wiring with
constructor-injected repositories for users, passwords, WebAuthn credentials,
and refresh tokens.
