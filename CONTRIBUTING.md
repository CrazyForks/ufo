# Contributing to UFO

Thanks for your interest! UFO is a monorepo with three apps: a Go API
(`apps/api`), a Next.js web UI (`apps/web`), and a Rust CLI (`apps/rover`).
The Go Hub owns its SQL (`apps/api/internal/{migrate/migrations,db/queries}`)
and the OpenAPI contract (`apps/api/internal/spec`), both embedded in the
binary.

## Getting set up

```bash
# Docker (live watch): PostgreSQL + API + web
scripts/dev.sh up
# then sign up at http://localhost:3000 and approve a rover from the browser
scripts/dev.sh rover enroll
```

See [README.md](README.md) for the run guide and configuration.

Toolchain for host-side work: Go ≥ 1.26, Node ≥ 20.9, Rust/Cargo, and `sqlc`
(only if you change SQL).

## Before you open a pull request

One call (recommended):

```bash
scripts/verify.sh
```

Suites: `diff`, `api`, `web`, `rover`, `openapi` (default all of
those). Optional: `sqlc` (regenerate and fail if dirty). Examples:
`scripts/verify.sh api web`, `scripts/verify.sh rover`.

Skip the web production build with `UFO_CHECK_SKIP_WEB_BUILD=1` when you
only need typecheck.

Equivalent manual commands:

```bash
# api
(cd apps/api && test -z "$(gofmt -l .)" && go build ./... && go vet ./... && go test ./...)

# web
(cd apps/web && npm ci && npm run lint && npm run build)

# rover
(cd apps/rover && cargo fmt --check && cargo clippy -- -D warnings && cargo test && cargo build)

# protocol (if you changed an endpoint)
npx --yes @redocly/cli@2.36.0 lint apps/api/internal/spec/openapi.yaml
```

CI runs the same class of checks on protected branches.

Keep related generated and documentation changes in the same pull request:

- SQL changes: add a migration under
  `apps/api/internal/migrate/migrations/`, run
  `go generate ./internal/migrate` (updates `migrations.sum`), edit
  `apps/api/internal/db/queries/*.sql`, run `sqlc generate`, and commit the
  generated `apps/api/internal/db` files and `migrations.sum`.
- API changes: update `apps/api/internal/spec/openapi.yaml` and lint it.
- CLI or setup changes: update [README.md](README.md) or
  [apps/rover/README.md](apps/rover/README.md) when commands or behavior
  change.

## Pull request checklist

- The change is scoped to one behavior, bug, or documentation topic.
- Fleet scoping, rover authorization, and credential handling remain intact.
- New or changed API behavior is reflected in OpenAPI.
- SQL changes include regenerated sqlc output.
- User-facing commands, environment variables, and workflows are documented.
- Relevant checks from this file pass locally, or the PR explains why they
  were not run.

## Conventions

- **Commits:** use [Gitmoji](https://gitmoji.dev) followed by a concise,
  imperative summary, for example `✨ Add operation labels`.
- **Database:** add migrations under
  `apps/api/internal/migrate/migrations/`; `go generate ./internal/migrate`;
  edit queries; `sqlc generate`; commit SQL, `migrations.sum`, and generated
  `apps/api/internal/db` changes.
- **API contract:** if you add or change an endpoint, update
  [`apps/api/internal/spec/openapi.yaml`](apps/api/internal/spec/openapi.yaml)
  in the same pull request.
- **Comments:** keep them terse. Explain *why*, not *what*.
- **Documentation:** wrap Markdown prose greedily at 78 columns. Code blocks,
  tables, and long URLs/paths are exempt; third-party notices stay verbatim.
- **Security:** never weaken fleet scoping or the rover/credential
  authorization paths without discussion. See [SECURITY.md](SECURITY.md) for
  the trust model.

## Reporting issues

Use the GitHub issue templates. For security vulnerabilities, follow
[SECURITY.md](SECURITY.md) instead of opening a public issue.
