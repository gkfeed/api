# Remove v1 library routes after client migration

The v2 feed and item resource routes supersede the v1 library routes. Keep the
v1 routes available until both front and gkbot have migrated and their deployed
versions no longer call them.

## Prerequisites

- [ ] Front uses `/api/v2/feeds` and `/api/v2/items`, including feed creation
      with explicit `title`, `type`, and `url` instead of `/api/v1/add_lazy`.
- [ ] Gkbot uses the v2 routes and explicit feed creation where needed.
- [ ] Confirm the deployed clients have stopped calling the superseded v1
      library routes. Announce the removal before deploying it.

## Removal

- [ ] Remove `/api/v1/list`, `/api/v1/add`, `/api/v1/add_lazy`,
      `/api/v1/delete`, `/api/v1/get_items`, `/api/v1/item`, and
      `/api/v1/items/{id}` after the prerequisites are met.
- [ ] Review `/api/v1/add_deleted_items` separately. Its existing `410 Gone`
      compatibility contract and removal condition are documented in
      `docs/library-storage-follow-ups.md`.
- [ ] Update the README, Swagger, and route tests when the routes are removed.

The RSS `/api/v1/feed`, `/api/v1/feed_types`, and v1 auth routes are outside
this removal task. Their replacements need a separate decision.
