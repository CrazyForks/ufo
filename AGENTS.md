# UFO Agent Instructions

Project conventions, not suggestions. Read before changing code.

## Operating Posture

- Don't guess. Inspect the code, schema, generated files, tests, and git state
  before acting.
- UFO is pre-release. Keep changes scoped, but refactor bad structure cleanly
  rather than preserving it.
- No compatibility shims for old data, APIs, storage paths, or generated
  artifacts unless asked.
- Prefer the simplest design that fully solves the problem — no speculative
  abstractions, unused config, or extra layers.
- Reuse existing patterns; introduce new ones deliberately and small.
- Never revert unrelated worktree changes — treat them as other agents' work.
- If a response references a generated report or attachment, create the real
  file first and link its actual repo/workspace path or asset URL.
- Keep experimental product settings in JSON `metadata`; promote to typed
  columns only once the behavior is stable enough to justify a migration.

## Database & Migrations

- Edit `apps/api/internal/migrate/migrations/0001_init.sql` directly; no new
  migration unless asked.
- Run `sqlc generate` after schema or query changes; never hand-edit generated
  DB files.
- Timestamps are `timestamptz`, stored UTC; the UI handles local display.
- Timestamp column order: `created_at`, `updated_at`, then domain `*_at`
  (`started_at`, `finished_at`, `heartbeat_at`).
- Source SQL in `db/queries/queries.sql` uses `sqlc.arg(name)`, not `$1`.
  Quote keyword args: `sqlc.arg('limit')`.
- No `SELECT *`, `table.*`, or `RETURNING *` — list columns in table order.
- Prefer one clear JOIN over multiple round trips when data is needed
  together.
- Name result aliases meaningfully (`count`, not `n`).

## API Design

- Follow REST: resource paths for identity, bodies for create/update, query
  params for GET list filters.
- No `fleet_id` query param on mutating APIs — use a body field or resource
  path.
- Don't force long-lived connections (WebSocket) into per-fleet REST nesting.
- Use full words where abbreviations are ambiguous (`websocket` over `ws`);
  avoid generic names like `hub` that collide with the domain.
- No duplicate APIs per UI location when the resource is global.

## Assets & Artifacts

- `assets` holds real files/blobs only. Text — comms, operation bodies,
  comments, pilot final messages, telemetry, logs — stays in the database.
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
- Don't inline attached bytes into pilot prompts — pass asset URLs/metadata
  and let the rover fetch.
- Object-store keys use public UUIDs, UTC dates, and shards — no filenames
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
- Keep operational UI dense, aligned, and predictable — no marketing sections,
  decorative cards, or one-off palettes.
- Text must fit its controls on desktop and mobile; use stable dimensions for
  counters, pills, tiles, boards, and toolbars.

## Rover & CI

- All cross-platform rover builds are Rover tests — no "default platform" vs
  "cross" split.
- Platform doc order: macOS, FreeBSD, Linux, Windows. Use product OS names in
  user-facing text.
- No unsafe temp paths like `/tmp/ufo` — use the configured local root, user
  data dir, or OS temp.
- Rover operation directories are sharded and date-partitioned.

## Documentation

- Wrap Markdown prose greedily at 78 columns (fill each line before breaking).
  This applies to text only — code blocks, tables, and unbreakable tokens
  (URLs, paths) are exempt.
- `THIRD_PARTY_NOTICES.md` reproduces third-party license texts and must stay
  verbatim.

## Verification

Run the narrowest meaningful tests, broaden when touching shared behavior:

- `sqlc generate`
- `GOCACHE="${TMPDIR:-/tmp}/ufo-gocache" go test ./...` (from `apps/api`)
- `npm run lint` (from `apps/web`)
- relevant `cargo` checks/tests (from `apps/rover`)
- `git diff --check`

If a sandbox blocks the default Go cache, point `GOCACHE` at a writable temp
dir (`${TMPDIR:-/tmp}`) rather than skipping tests.
</content>
</invoke>
