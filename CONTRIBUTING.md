# Contributing to UFO

Thanks for your interest! UFO is a monorepo with three apps — a Go API
(`apps/api`), a Next.js web UI (`apps/web`), and a Rust rover (`apps/rover`).
The Go Hub owns its SQL (`apps/api/internal/{migrate/migrations,db/queries}`) and
the OpenAPI contract (`apps/api/internal/spec`), both embedded in the binary.

## Getting set up

```bash
scripts/dev.sh up        # PostgreSQL + automatically rebuilt API + web in Docker
# then sign up at http://localhost:3000 and start a rover on the host:
UFO_ROVER_ENROLLMENT_CODE=<code> scripts/dev.sh rover
```

See [README.md](README.md) for the full run guide and configuration.

Toolchain for host-side work: Go ≥ 1.26, Node ≥ 20.9, Rust/Cargo, and `sqlc`
(only if you change SQL).

## Preview development

During the MVP preview, schema changes are folded into
`apps/api/internal/migrate/migrations/0001_init.sql`; development database resets are expected. For the
Docker stack, reset with `scripts/dev.sh down -v`.

## Before you open a pull request

Run the checks for whatever you touched:

```bash
# api
(cd apps/api && go build ./... && go vet ./... && go test ./...)

# web
(cd apps/web && npm ci && npm run lint && npm run build)

# rover
(cd apps/rover && cargo fmt --check && cargo clippy -- -D warnings && cargo test && cargo build)

# protocol (if you changed an endpoint)
npx --yes @redocly/cli@2.34.0 lint apps/api/internal/spec/openapi.yaml
```

CI runs these on every pull request.

## Conventions

- **Commits:** use [Gitmoji](https://gitmoji.dev) followed by a concise,
  imperative summary, for example `✨ Add operation labels`.
- **Database:** edit `apps/api/internal/db/queries/*.sql`, run `sqlc generate`, and commit the
  generated `apps/api/internal/db` changes.
- **API contract:** if you add or change an endpoint, update
  [`apps/api/internal/spec/openapi.yaml`](apps/api/internal/spec/openapi.yaml) in the same
  pull request.
- **Comments:** keep them terse — explain *why*, not *what*.
- **Security:** never weaken fleet scoping or the rover/credential authorization
  paths without discussion. See [SECURITY.md](SECURITY.md) for the trust model.

## Reporting issues

Use the GitHub issue templates. For security vulnerabilities, follow
[SECURITY.md](SECURITY.md) instead of opening a public issue.
