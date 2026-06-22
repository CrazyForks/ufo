#!/usr/bin/env bash
# Bring up the UFO dev stack.
#
# Usage:
#   scripts/dev.sh up        # docker (live watch): PostgreSQL + API + web
#   scripts/dev.sh down      # stop the docker stack
#   scripts/dev.sh db        # PostgreSQL only (docker), wait for health
#   scripts/dev.sh api       # host: Go Hub (needs db up)
#   scripts/dev.sh web       # host: Next.js web board (needs api up)
#   scripts/dev.sh rover     # host: Rust rover (needs api up)
#
# `up` is the default. The host commands are the all-local fallback (Go + Node).
# Env defaults come from .env if present.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

UFO_ROVER_ENROLLMENT_CODE_WAS_SET=0
if [[ "${UFO_ROVER_ENROLLMENT_CODE+x}" == "x" ]]; then
  UFO_ROVER_ENROLLMENT_CODE_ENV="$UFO_ROVER_ENROLLMENT_CODE"
  UFO_ROVER_ENROLLMENT_CODE_WAS_SET=1
fi

# Load .env if present (export all keys).
if [[ -f .env ]]; then
  set -a; # shellcheck disable=SC1091
  source .env; set +a
fi
if [[ "$UFO_ROVER_ENROLLMENT_CODE_WAS_SET" == 1 ]]; then
  export UFO_ROVER_ENROLLMENT_CODE="$UFO_ROVER_ENROLLMENT_CODE_ENV"
fi

: "${UFO_HUB_DATABASE_URL:=postgres://ufo:ufo@localhost:5432/ufo?sslmode=disable}"
: "${UFO_HUB_BIND:=:8080}"
: "${UFO_HUB_UPLINK:=http://localhost:8080}"
: "${UFO_HUB_ORIGINS:=http://localhost:3000,http://127.0.0.1:3000}"
export UFO_HUB_DATABASE_URL UFO_HUB_BIND UFO_HUB_UPLINK UFO_HUB_ORIGINS

cmd="${1:-}"
case "$cmd" in
  db)
    docker compose up -d --wait postgres   # --wait blocks until healthy
    echo "PostgreSQL is healthy at $UFO_HUB_DATABASE_URL"
    ;;
  api)
    cd apps/api
    go run ./cmd/api
    ;;
  rover)
    # First run: UFO_ROVER_ENROLLMENT_CODE=<code> scripts/dev.sh rover  (enrolls a new
    # rover under its id, then starts). After that, `scripts/dev.sh rover` runs all
    # enrolled rovers (~/.ufo/rovers.json) concurrently.
    cd apps/rover
    if [[ -n "${UFO_ROVER_ENROLLMENT_CODE:-}" ]]; then
      cargo run -- rover enroll --hub "${UFO_HUB_UPLINK}" --enrollment-code "${UFO_ROVER_ENROLLMENT_CODE}"
    fi
    cargo run -- rover start
    ;;
  web)
    cd apps/web
    [[ -d node_modules ]] || npm install
    npm run dev
    ;;
  up|"")
    # Builds dev images, starts the stack, and live-syncs source on change
    # (next dev Fast Refresh; go run restarts on edit).
    docker compose up --build --watch "${@:2}"
    ;;
  down)
    docker compose down "${@:2}"
    ;;
  *)
    echo "usage: scripts/dev.sh {up|down|db|api|web|rover}" >&2
    echo "  up        docker (live watch): PostgreSQL + API + web   [default]" >&2
    echo "  down      stop the docker stack" >&2
    echo "  db        docker: PostgreSQL only" >&2
    echo "  api       host: Go Hub (go run)" >&2
    echo "  web       host: Next.js dev server" >&2
    echo "  rover     host: Rust rover (cargo run)" >&2
    exit 2
    ;;
esac
