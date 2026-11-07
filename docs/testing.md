# Testing

Run the local suite with `make test`, or all quality checks with `make check`.
Both generate Swagger before compiling the API package. To run a subset:

```sh
make swagger
cd app
go test ./cmd/api -run 'TestItemSync|TestItemChanges|TestSyncRejects'
go test -race ./...
```

## HTTP scenarios

Tests in `app/cmd/api` send requests through `newHandler`, including routing,
authentication, middleware, services, repositories, and JSON or XML encoding.
Each scenario passes its own temporary SQLite database with the shared test
schema. No running server, network access, or API key is required.

For a behavior that crosses these components, add a scenario here. Create a
subscription through HTTP, read it back, repeat the request, and check what
another user can see. For pagination, follow the returned cursor and check the
complete result for missing, duplicate, or foreign items. Treat sync cursors as
opaque strings: use them in later requests instead of decoding them in tests.

SQL fixtures supply users and parser-owned items, which have no API creation
route. They also simulate parser inserts and updates between sync requests.
Use a SQLite trigger to inject a storage failure when testing transaction
rollback, then read the library through HTTP to check that data survived.
Substitute external services such as feed type detection with
`newHandlerWithLibrary`; keep routing, authentication, application services and
repositories real.

Each scenario creates its own database, so assertions must not depend on
process-global state. Individual scenarios can still send concurrent requests
against the same handler, as the concurrent creation tests do.

## Smaller tests

Keep focused tests for rules with useful independent inputs and outputs, such
as password hashing, expiry, URL normalization, and rejecting private network
addresses. External HTTP adapters also need request and response contract tests.
Avoid tests whose only assertion is a delegated argument, a call count, or a
copy of a production constant. A change to internal helper structure should not
require rewriting a behavior test.

## PostgreSQL

SQLite scenarios do not verify PostgreSQL isolation, grants, or infra-owned DDL.
The repository contract suite runs against both local SQLite fixtures and an
opt-in disposable PostgreSQL database. See the
[PostgreSQL contract test instructions](../README.md#postgresql-contract-tests)
for provisioning and `GKFEED_TEST_DATABASE_URL`. Those PostgreSQL tests truncate
application data and must only run against a disposable database.
