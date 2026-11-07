# Browser session contract

The Go API owns browser sessions. The browser calls `/api/...` through the
frontend's public origin. Passwords and session identifiers are never returned
in browser login JSON. Password and passkey login return only `{id, name}`.

| Method | Route | Success | Authentication |
| --- | --- | --- | --- |
| POST | `/api/v2/sessions` | `201`, user and cookie | JSON `{username, password}` |
| POST | `/api/v2/sessions/passkey/begin` | `200`, WebAuthn assertion options | None |
| POST | `/api/v2/sessions/passkey/finish` | `201`, user and cookie | WebAuthn assertion response |
| GET | `/api/v2/me` | `200`, user and renewed cookie | Session cookie |
| DELETE | `/api/v2/sessions/current` | `204`, cleared cookie | Current cookie, if present |

`GET /api/v2/me` returns `401` for a missing, invalid, revoked, or expired
session. It does not accept Basic or Bearer authentication. Invalid credentials
return `401`; malformed login JSON returns `400`. Passkey endpoints return `503`
when WebAuthn is not configured. Reusing a completed passkey challenge returns
`401`. Responses use `Cache-Control: no-store`.

## Cookie and expiry

The API sets `gkfeed.session` with `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`,
and no `Domain`. Its value is a random 256-bit identifier. PostgreSQL stores
only its SHA-256 digest. The cookie has `Max-Age` and `Expires`, so closing the
browser does not end the session.

A session expires after 30 days without an authenticated request or 90 days
since creation, whichever comes first. Successful cookie authentication updates
`last_seen_at` and renews the cookie through the earlier of those deadlines.
The absolute expiry never changes. Server expiry is authoritative.

Logout deletes the current row and clears the cookie with the same attributes.
Repeating logout, including without a cookie, returns `204`. Other devices keep
their sessions. The existing `/api/v1/auth/logout-all` also revokes all browser
sessions for that user.

Expired rows do not authenticate. Infra can periodically delete rows where
`expires_at <= CURRENT_TIMESTAMP` or
`last_seen_at <= CURRENT_TIMESTAMP - INTERVAL '30 days'`.

## Origin, CORS and v1 compatibility

Every v2 login or logout request requires exactly one `Origin` header matching
an entry in `GKFEED_ALLOWED_ORIGINS`, even with Basic/Bearer credentials.
Missing, duplicate, `null`, foreign or wildcard origins return `403` as
`text/plain`. Use full HTTP(S) origins with scheme and port, without credentials,
a path, query, fragment or trailing slash. The proxy must preserve `Origin`;
the API does not infer trust from `Host` or forwarded headers.

Protected v1 routes and all v2 feed/item routes, including sync/changes, accept
`gkfeed.session`. Cookie authentication requires the same Origin check before
authentication for methods other than `GET`, `HEAD` and `OPTIONS`. Reads do not
require Origin. A nonempty `Authorization` header selects Basic/Bearer instead,
without the cookie Origin check; invalid header credentials never fall back
to a valid cookie. `GET /api/v2/me` accepts only the cookie.

CORS uses `GKFEED_ALLOWED_ORIGINS`, allows credentials and request headers,
and permits `GET`, `HEAD`, `POST` and `DELETE`. Allowed origins receive
`Access-Control-Allow-Origin` and `Access-Control-Allow-Credentials: true`.
Preflight `OPTIONS` returns `204` without authentication, including on rejection;
check the permission headers, not status alone. Actual requests still reach
the router when CORS denies permission. The cookie Origin guard separately
rejects unsafe cookie requests. CORS wildcard matching does not authorize them.
Cross-origin browser clients must include credentials; CORS does not override
`Secure` or `SameSite=Lax`.

The cookie attributes follow the [MDN cookie reference](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie).
Exact Origin checks follow [OWASP's CSRF guidance](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html).

## PostgreSQL prerequisite for infra

This repository implements the API and tests. Infra must deliver the following
schema and privileges in a migration before this API is deployed. The SQL below
is the required contract, not a migration executed by the API:

```sql
CREATE TABLE public.browser_sessions (
    token_hash BYTEA PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id INTEGER NOT NULL REFERENCES public.users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CHECK (last_seen_at >= created_at),
    CHECK (expires_at > created_at)
);
CREATE INDEX browser_sessions_user_id_idx ON public.browser_sessions (user_id);
CREATE INDEX browser_sessions_expires_at_idx ON public.browser_sessions (expires_at);

GRANT SELECT, INSERT, UPDATE, DELETE ON public.browser_sessions TO gkfeed_api;
```

Startup checks the existing canonical migration registry and the required
browser-session columns. Missing schema prevents startup. The application role
must have `UPDATE` for activity renewal; the existing refresh-token grants are
insufficient. DDL stays in infra. User deletion remains outside this API's scope.

## Frontend follow-up

After infra and API are deployed, replace saved credentials with cookie login,
delete `gkfeed.credentials`, and require one fresh login. Restore login with
`GET /api/v2/me`. Calls on the same public origin use normal browser cookie
handling; no token storage or refresh endpoint is needed.

Keep the frontend's current `make dev` workflow and hosted API behind its dev
proxy. Only that dev proxy should remove `Secure` from the session cookie when
serving local HTTP, while retaining `HttpOnly`, `SameSite`, expiry and deletion
attributes. Preserve `Origin` and explicitly configure each permitted local
origin on the API. The Go API always emits `Secure`; it has no insecure-cookie
production switch. Passkey testing uses HTTPS or a browser-supported localhost
secure context.

## Legacy token ordering

Password/passkey token issuance, refresh rotation, logout, logout-all and reuse
revocation share a lifecycle lock in the access-session store. The lock spans
database commit and the matching in-memory publication or revocation. A refresh
cannot publish an access token after a concurrent revocation has completed.
The PostgreSQL advisory transaction lock still orders database changes.
Legacy access tokens remain local to the issuing API process. Browser sessions
use PostgreSQL for every authentication and can be shared by API processes.
