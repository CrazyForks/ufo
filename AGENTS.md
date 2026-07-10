# UFO Agent Instructions

Project conventions, not suggestions. Read before changing code.

This file is the durable source of agent rules. Do not claim memory or
enforcement beyond what is written here and what is verified in-session.

## Hard gates

Violate any of these and the task is not done:

1. **Comments:** default is zero new comments. No narration, no essays, no
   JSDoc restating types. Add a comment only when the next reader would
   mis-operate without it, and keep it one short line. Strip comments you
   added while coding before finishing.
2. **Verify before done:** run the real commands for every surface you
   touched, then report outcomes. Do not claim success without running
   them. Prefer one call when several surfaces changed:
   `scripts/verify.sh` (or `scripts/verify.sh api web rover` ...).
   - API Go: `GOCACHE="${TMPDIR:-/tmp}/ufo-gocache" go test` on packages
     you changed (widen to `./...` when shared).
   - Rover: `cargo fmt --check`, `cargo clippy -- -D warnings`, and
     `cargo test` for relevant tests (full suite when forge/main/tests
     change).
   - Web: `npm run lint` (tsc) from `apps/web`.
   - Also `git diff --check` when you edited files.
3. **Finish the shape:** nested config and multi-field designs are
   implemented end-to-end in one change (API parse, stamp, readers, UI,
   i18n, tests). No one-field stubs unless the user cut scope.
4. **No legacy theater for unshipped work:** no dual-read, no migrate-on-
   write of removed keys, no error text that documents deleted fields.
   Pre-release: only the current shape exists.
5. **Tests and fixtures use product vocabulary only.** No private
   nicknames, ad-hoc branch names from chat, or non-product labels in
   tests, sample metadata, or committed prompts.
6. **Do not start Hub or rover, and do not invent credentials,** unless
   the user asks. The user owns runtime enrollment and secrets.
7. **Do not second-guess the user's runtime** (binary version, token
   state, process uptime) after they already stated it. Fix code or act
   on the stated state.

## Operating Posture

- Don't guess. Inspect the code, schema, generated files, tests, and git state
  before acting.
- UFO is pre-release. Keep changes scoped, but refactor bad structure cleanly
  rather than preserving it.
- No compatibility shims for old data, APIs, storage paths, or generated
  artifacts unless asked.
- Prefer the simplest design that fully solves the problem. No speculative
  abstractions, unused config, or extra layers.
- Reuse existing patterns; introduce new ones deliberately and small.
- Never revert unrelated worktree changes. Treat them as other agents' work.
- If a response references a generated report or attachment, create the real
  file first and link its actual repo/workspace path or asset URL.
- Keep experimental product settings in JSON `metadata`; promote to typed
  columns only once the behavior is stable enough to justify a migration.
- Use UFO vocabulary in code, APIs, and user-facing text: fleet, mission,
  rover, pilot, crew, operation, run, routine, signal, asset.
- Do not edit gitignored local secrets (`.env`) unless the user asks. Change
  `.env.example`, `.env.production.example`, and docs instead.

## Database & Migrations

- Schema changes: add a new file under
  `apps/api/internal/migrate/migrations/` (e.g.
  `9527_issue_lifetime_peon_badge.sql`).
  Do not rewrite applied migrations (DB `schema_migrations` checksums) or
  edit SQL without regenerating `migrations/migrations.sum`
  (`go generate ./internal/migrate` from `apps/api`).
- After any edit to `apps/api/internal/db/queries/` or schema SQL sqlc
  loads, run `sqlc generate` from the repo root and commit the generated
  files under `apps/api/internal/db/` (except `queries/`) in the same
  change. Never hand-edit those generated files.
- Timestamps are `timestamptz`, stored UTC; the UI handles local display.
- Timestamp column order: `created_at`, `updated_at`, then domain `*_at`
  (`started_at`, `finished_at`, `heartbeat_at`).
- Source SQL in `apps/api/internal/db/queries/queries.sql` uses
  `sqlc.arg(name)`, not `$1`. Quote keyword args: `sqlc.arg('limit')`.
- Keep `-- name: QueryName :one|:many|:exec` immediately above the SQL it
  names. Put explanatory comments *above* the `-- name:` line, never between
  `-- name:` and the statement.
- No `SELECT *`, `table.*`, or `RETURNING *`. List columns in table order.
- Prefer one clear JOIN over multiple round trips when data is needed
  together.
- Name result aliases meaningfully (`count`, not `n`).

## API Design

- Follow REST: resource paths for identity, bodies for create/update, query
  params for GET list filters.
- No `fleet_id` query param on mutating APIs. Use a body field or resource
  path.
- Don't force long-lived connections (WebSocket) into per-fleet REST nesting.
- Use full words where abbreviations are ambiguous (`websocket` over `ws`);
  avoid generic names like `hub` that collide with the domain.
- No duplicate APIs per UI location when the resource is global.
- When adding or changing HTTP endpoints, update
  `apps/api/internal/spec/openapi.yaml` in the same change and lint it
  (see CONTRIBUTING.md).

## Auth, Tenancy & Capacity

- A fleet is the trust boundary for rover code execution. Preserve
  fleet-scoped membership checks on every tenant resource.
- Enforce authorization and capacity on the Hub, not only in clients: rover
  `units` on accept, fleet membership, and credential checks must not rely on
  rover or browser honesty alone.
- Production requires `UFO_HUB_JWT_PRIVATE_KEY`. Ephemeral signing keys are
  allowed only with explicit `UFO_HUB_JWT_ALLOW_EPHEMERAL=1`
  (local/dev/tests). Do not enable that flag in production examples or deploy
  defaults.

## Assets & Artifacts

- `assets` holds real files/blobs only. Text (comms, operation bodies,
  comments, pilot final messages, telemetry, logs) stays in the database.
- Text artifacts like `git.diff` stay in the database. List/detail APIs return
  metadata + preview; full content comes from a dedicated content endpoint.
- Uploads and paste/drop files are global fleet intake, not per-`operation`/
  `comment`/`routine` concepts. Record operation context for visibility, but
  don't model a separate attachment relation unless a design requires it.
- Rover/pilot files become assets only when they're real generated files.
  Don't upload the workspace.
- For a pilot-referenced rover-local file: validate the path is inside the
  operation directory, enforce size/type/count limits, upload it as an asset,
  and rewrite the message to the asset URL before posting.
- Don't inline attached bytes into pilot prompts. Pass asset URLs/metadata
  and let the rover fetch.
- Object-store keys use public UUIDs, UTC dates, and shards. No filenames
  (those live in DB metadata/columns):
  - `v1/fleets/{fleet_id}/uploads/{YYYY}/{MM}/{DD}/{asset_shard}/{asset_id}`
  - `v1/fleets/{fleet_id}/runs/{YYYY}/{MM}/{DD}/{run_shard}/{run_id}/artifacts/{asset_shard}/{asset_id}`
  - `v1/users/{user_id}/uploads/{YYYY}/{MM}/{DD}/{asset_shard}/{asset_id}`
- Support local, S3-compatible, and GCS backends through the asset store
  abstraction; keep vendor branching at the backend boundary.

## User Interface

- Icon buttons communicate current state where the surrounding UI does; keep
  icon semantics consistent.
- Don't show disabled preview icons for unpreviewable files.
- Attachment panels stay hidden when empty and open by default when assets
  exist; remember expanded/collapsed and list/grid/compact view preferences.
- Show uploaded assets as tiles/chips, not raw download links.
- Operation pages accept pasted clipboard files even with no editor focused;
  keep uploaded assets visible for later linking.
- Keep operational UI dense, aligned, and predictable. No marketing sections,
  decorative cards, or one-off palettes.
- Text must fit its controls on desktop and mobile; use stable dimensions for
  counters, pills, tiles, boards, and toolbars.
- User-rendered markdown links must allowlist safe schemes (relative paths,
  `http:`, `https:` only). Do not pass through `javascript:`, `data:`, or
  other schemes as navigable `href`s.

## Rover & CI

- All cross-platform rover builds are Rover tests. No "default platform" vs
  "cross" split.
- Platform doc order: macOS, FreeBSD, Linux, Windows. Use product OS names in
  user-facing text.
- No unsafe temp paths like `/tmp/ufo`. Use the configured local root, user
  data dir, or OS temp.
- Rover operation directories are sharded and date-partitioned.

## Documentation

- Wrap Markdown prose greedily at 78 source columns (not display width). Keep
  filling the line until the next break unit (word, wide character, inline
  code/link, or punctuation group) would exceed 78. Text only; code blocks,
  tables, diagrams, badges, and unbreakable tokens (URLs, paths) are exempt.
- `THIRD_PARTY_NOTICES.md` reproduces third-party license texts and must stay
  verbatim.
- Document new or changed user-facing env vars and workflows in README (and
  `.env*.example` when applicable) in the same change.

## Verification

Same as Hard gate #2. Prefer:

```bash
scripts/verify.sh                 # full local gate
scripts/verify.sh api web rover   # subset
scripts/verify.sh sqlc            # after query/schema edits
```

Narrowest meaningful tests first; broaden when touching shared behavior.
`scripts/verify.sh` sets `GOCACHE` to `${TMPDIR:-/tmp}/ufo-gocache` by
default. Details: CONTRIBUTING.md.

Integration-style API tests (e.g. `authz_test.go`) need
`UFO_HUB_TEST_DATABASE_URL` (must not be the runtime Hub DB); if unset they
skip. Say so rather than claiming full coverage.

Before the final assistant message on a code change: actually run the
commands, fix failures, then report what ran and the outcome. No credit for
intent.
