# UFO: Unified Fleet Orchestrator

**An open-source zero-human ops platform** 🦾🩶

[![Build](https://img.shields.io/github/actions/workflow/status/fengsi/ufo/ci.yml?logo=github&style=for-the-badge)](https://github.com/fengsi/ufo/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/fengsi/ufo?style=for-the-badge)](https://github.com/fengsi/ufo/releases)
[![crates.io](https://img.shields.io/crates/v/ufo-cli?style=for-the-badge)](https://crates.io/crates/ufo-cli)
[![License](https://img.shields.io/github/license/fengsi/ufo?style=for-the-badge)](LICENSE)
[![Status](https://img.shields.io/badge/status-preview-blue?style=for-the-badge)](CHANGELOG.md)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&style=for-the-badge)](apps/api/go.mod)
[![Node](https://img.shields.io/badge/Node-20.9%2B-5FA04E?logo=node.js&style=for-the-badge)](apps/web/package.json)
[![Rust](https://img.shields.io/badge/Rust-2024-B7410E?logo=rust&style=for-the-badge)](apps/rover/Cargo.toml)
[![Gitmoji](https://img.shields.io/badge/commits-gitmoji-FDD563?style=for-the-badge)](https://gitmoji.dev)

UFO is an operations board that keeps execution on enrolled rovers. The Hub
tracks fleets, missions, conversations, runs, and review handoffs; rovers are the
host-side runtimes that do the work. A **pilot** is a local AI CLI that drives a rover
(built-ins include Claude Code, Codex, Antigravity, Cursor Agent, GitHub Copilot,
Amp Code, OpenCode, OpenClaw, Hermes, Pi, Kimi, and Kiro):
assign an operation to a pilot and any rover it can drive in the fleet picks it
up, works in an isolated directory, streams progress, and returns a final message
plus git diff for review.

> [!WARNING]
> **MVP preview:** UFO's main workflow is functional, but compatibility is not
> guaranteed yet. APIs, the database schema, configuration, and rover protocol
> may change without notice. Upgrading may require resetting the database; a
> migration path between commits or releases is not guaranteed. Do not use this
> preview for data you cannot afford to lose.

See [`CHANGELOG.md`](CHANGELOG.md) for release history.

---

## Architecture

UFO is multi-tenant: users sign in, and **fleets** scope all data. **Missions**
group related operations and provide short keys like `MSJ`, producing operation
codes such as `MSJ-123`.

```
Hub

┌─────────────┐    ┌─────────────┐    ┌─────────────┐       ┌─────────────┐
│ Browser     │◀──▶│ Next.js web │◀──▶│ Go Hub      │◀─SQL─▶│ PostgreSQL  │
│ operations  │    │ /api/v1     │    │ /v1 API     │       │             │
│ board       │    │ facade      │    │             │       │             │
└─────────────┘    └─────────────┘    └──────┬──────┘       └─────────────┘
                                             │ fleet-scoped HTTP
                                             │ claims / progress
                                             │ results / artifacts
                                             │
Rover host
                                             ↕
                                      ┌─────────────┐
                                      │ Pilot       │
                                      │ drives      │
                                      │ Rust rover  │
                                      └──────┬──────┘
                                             │ works in
                                             ▼
                               ┌───────────────────────────┐
                               │ operation work directory  │
                               └───────────────────────────┘
```

- **`apps/web`** — Next.js product UI: a default drag-and-drop **Kanban** board
  plus **List** and **Lanes** views; operation detail pages with conversations,
  live run timelines, diffs, labels, reactions, sub-operations, relationships,
  and **Signals**. Proxies `/api/v1`.
- **`apps/api`** — Go Hub (pgx + sqlc): auth, fleets, memberships,
  invitations, pilots, crews, operations, comments, runs, artifacts, missions,
  labels, reactions, signals, rover enrollment, and connection-token endpoints.
- **`apps/rover`** — Rust CLI rover: enrolls via an enrollment code, long-poll
  claims runs, lets the assigned pilot drive the rover, streams typed messages, uploads a
  `git diff`, and reports terminal state. One host can hold many enrollments.
- **`apps/api/internal/migrate/migrations`, `apps/api/internal/db/queries`** —
  SQL migrations (embedded) and sqlc queries.
- **[`apps/api/internal/spec/openapi.yaml`](apps/api/internal/spec/openapi.yaml)** —
  OpenAPI source of truth; embedded and served at `/openapi.yaml`.

### Capabilities

- **Accounts + tenancy:** email/password + cookie sessions; **fleets** +
  memberships scope every entity; invite teammates by email (owner/admin/member).
- **Rovers as teammates:** each rover has its own connection token, reports
  online/busy/offline status, and receives work only when its tags match.
- **Pilots drive rovers:** a pilot is a local AI CLI that drives a rover; assign an
  operation to a pilot and a capable fleet rover claims it.
  Crews can include pilots and humans; assigning to a pilot or pilot-backed crew
  auto-dispatches, while human-only work stays in **backlog**. If the pilot has no
  rover to drive in the fleet, the operation is blocked with a signal instead of
  queueing forever.
- **Operations as conversations + review handoff:** pilots work in resumable
  sessions, stream typed telemetry, return a diff artifact, and hand successful
  runs to **In Review** instead of auto-closing them.
- **Planning dates vs lifecycle time:** operation `start_date` / `due_date` are
  editable planning dates; `started_at` / `finished_at` are UTC lifecycle
  timestamps set by status changes.
- **Board:** Kanban, List, and Lanes views with configurable columns, filters,
  sorting, labels, reactions, sub-operations, relationships, and signals for
  review handoffs or blocked work.
- **Real-time over PostgreSQL `LISTEN/NOTIFY`:** WebSocket UI updates and rover
  long-polling share the database as the coordination layer; no extra broker is
  required.
- **Orphaned-run lease:** rover heartbeats; an API sweeper requeues silent runs
  (`UFO_HUB_RUN_LEASE_SECONDS`, default 30).
- **Multi-instance-safe:** versioned migrator under a `pg_advisory_lock`, claim
  via `FOR UPDATE SKIP LOCKED`, events ordered by insertion id, stateless API.

> **Trust boundary:** anyone in a fleet can dispatch work to connected rovers.
> Pilots run local CLIs with the rover user's privileges. Use dedicated users or
> hosts for rovers, and read [`SECURITY.md`](SECURITY.md) before sharing a
> fleet.

---

## Prerequisites

- **Docker** — runs PostgreSQL, the API, and the web board.
- **Rust / Cargo** — the rover always runs on the host (it's the local runtime).

Only needed for the optional host-based dev path (running api/web without Docker):

- Go ≥ 1.26, Node ≥ 20.9 (Next 16 requires it), and `sqlc` (`brew install sqlc`,
  only if you change SQL).

## Running it

**Recommended — Docker for everything except the rover:**

```bash
scripts/dev.sh up        # docker (live watch): PostgreSQL + api + web
```

Source edits sync into the containers live — the web has Fast Refresh and the
API restarts on change (`docker compose watch`); no manual rebuild.

If a preview update changes `0001_init.sql`, reset the local database before
starting again:

```bash
scripts/dev.sh down -v   # deletes the local PostgreSQL volume and all UFO data
scripts/dev.sh up
```

1. Open <http://localhost:3000> and **sign up** — a fleet is created for you.
2. Open the **Rovers** panel → **Create enrollment code** → copy the `UFO_ROVER_ENROLLMENT_CODE=…` line.
3. Start the rover on the host (it's the local runtime — touches host files/tools —
   and reaches the Hub at `localhost:8080`). It enrolls on first run and
   stores each enrollment (keyed by rover id) in `~/.ufo/rovers.json`; later
   runs use the stored connection token:

   ```bash
   UFO_ROVER_ENROLLMENT_CODE=<code> scripts/dev.sh rover    # first run (enrolls + starts)
   scripts/dev.sh rover                                  # starts all enrolled rovers
   ```

   A host can hold many enrollments (across fleets/hubs); manage them with:

   ```bash
   # from the repo root (the rover crate lives in apps/rover):
   cargo run --manifest-path apps/rover/Cargo.toml -- rover list                      # show enrollments
   cargo run --manifest-path apps/rover/Cargo.toml -- rover status                    # check hub/token/auto-tags
   cargo run --manifest-path apps/rover/Cargo.toml -- rover remove <rover-id|prefix>  # remove one enrollment
   cargo run --manifest-path apps/rover/Cargo.toml -- rover remove --all              # remove all enrollments
   ```
4. Create a mission, then an operation on the board, assign it to a pilot, and
   watch the run move `queued → claimed → running → succeeded` live, with its diff
   artifact. The rover shows **online/busy/offline** in the Rovers panel.

**Alternative — everything on the host** (needs Go + Node ≥ 20.9 installed),
one process per terminal (`api`, `web`, then sign up and run `rover` with the
enrollment code):

```bash
# docker: PostgreSQL only
scripts/dev.sh db

# host: Go API
scripts/dev.sh api

# host: Next.js dev server
scripts/dev.sh web

# host: Rust rover (enrolls)
UFO_ROVER_ENROLLMENT_CODE=<code> scripts/dev.sh rover
```

### Configuration

Copy `.env.example` to `.env` to override defaults:

| Var | Default | Used by |
| --- | --- | --- |
| `UFO_HUB_DATABASE_URL` | `postgres://ufo:ufo@localhost:5432/ufo?sslmode=disable` | api |
| `UFO_HUB_BIND` | `:8080` | api |
| `UFO_HUB_RUN_LEASE_SECONDS` | `30` | api |
| `UFO_HUB_LONGPOLL_SECONDS` | `25` | api |
| `UFO_HUB_MAX_SUB_OPERATIONS` | `8` — max sub-operations a captain can propose at once | api |
| `UFO_HUB_SECURE_COOKIES` | _(unset)_ — set when serving over HTTPS | api |
| `UFO_HUB_UPLINK` | `http://localhost:8080` | rover, web (Hub origin; clients append `/v1`) |
| `UFO_HUB_ORIGINS` | _(unset)_ — CORS + WebSocket origin allowlist; set in production | api |
| `UFO_HUB_ROVER_ONLINE_WINDOW_SECONDS` | `60` — seconds since last contact before a rover is offline | api |
| `UFO_ROVER_ENROLLMENT_CODE` | _(from the Rovers panel; used by `rover enroll`)_ | rover |
| `UFO_ROVER_CONFIG` | `~/.ufo/rovers.json` — local enrollment store | rover |
| `UFO_ROVER_OUTPOST` | `~/.ufo` (operation work directories: `<outpost>/rovers/<rover-id>/operations/<operation-id>`) | rover |
| `UFO_ROVER_RETRY_SECONDS` | `1` — wait after a failed claim before retrying | rover |
| `UFO_ROVER_UNITS` | `1` — operations a rover runs at once (`--units`) | rover |
| `UFO_ROVER_HEARTBEAT_SECONDS` | `5` — run lease-renewal interval | rover |

### Regenerating DB code

After editing `apps/api/internal/migrate/migrations/*.sql` or `apps/api/internal/db/queries/*.sql`:

```bash
sqlc generate    # regenerates apps/api/internal/db
```

## Quick API smoke test (curl)

The UI surface needs a session cookie and a `?fleet=`. Public ids are strings,
so keep them quoted in JSON bodies.

```bash
# sign up (saves the session cookie); a fleet is created for you
curl -s -c jar -X POST localhost:8080/v1/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{"email":"me@example.com","password":"P@ssw0rd","name":"Me"}'

FLEET=$(curl -s -b jar localhost:8080/v1/fleets | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')

# a mission groups operations (required to create one)
MISSION=$(curl -s -b jar -X POST "localhost:8080/v1/missions?fleet=$FLEET" \
  -H 'Content-Type: application/json' -d '{"name":"Mission San Jose","key":"MSJ"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')

# discover available pilots, then assign an operation to one
# by id; it auto-dispatches once a rover advertising the matching pilot is online
curl -s -b jar "localhost:8080/v1/pilots?fleet=$FLEET"
curl -s -b jar -X POST "localhost:8080/v1/operations?fleet=$FLEET" \
  -H 'Content-Type: application/json' \
  -d "{\"title\":\"hello\",\"body\":\"Summarize this repo\",\"mission_id\":\"$MISSION\",\"assignee_type\":\"pilot\",\"assignee_id\":\"claude\"}"
curl -s -b jar "localhost:8080/v1/runs?fleet=$FLEET"            # runs in this fleet
```

## License

BSD 3-Clause. See [LICENSE](LICENSE) and
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
