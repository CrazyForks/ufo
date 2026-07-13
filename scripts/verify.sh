#!/bin/sh
# Run meaningful local checks (mirrors CONTRIBUTING / CI intent).
#
# Usage:
#   scripts/verify.sh              # all default suites
#   scripts/verify.sh api web      # selected suites
#   scripts/verify.sh rover
#   scripts/verify.sh openapi
#   scripts/verify.sh diff
#   scripts/verify.sh sqlc         # regenerate sqlc; fail if generated tree dirty
#   scripts/verify.sh list
#
# Env:
#   GOCACHE          default ${TMPDIR:-/tmp}/ufo-gocache
#   UFO_CHECK_SKIP_WEB_BUILD=1   skip npm run build in web suite
set -eu

ROOT="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export GOCACHE="${GOCACHE:-${TMPDIR:-/tmp}/ufo-gocache}"

step() {
  printf '\n==> %s\n' "$*"
}

run_diff() {
  step "git diff --check"
  git diff --check
  git diff --cached --check
}

run_api() {
  step "api: gofmt / build / vet / test"
  (
    cd apps/api
    unformatted="$(gofmt -l .)"
    if [ -n "$unformatted" ]; then
      echo "gofmt needed on:" >&2
      echo "$unformatted" >&2
      return 1
    fi
    go build ./...
    go vet ./...
    go test ./...
  )
}

run_web() {
  step "web: lint"
  (cd apps/web && npm run lint)
  if [ "${UFO_CHECK_SKIP_WEB_BUILD:-}" != "1" ]; then
    step "web: build"
    (cd apps/web && npm run build)
  fi
}

run_rover() {
  step "rover: fmt / clippy / test / build"
  (
    cd apps/rover
    cargo fmt --check
    cargo clippy -- -D warnings
    cargo test
    cargo build
  )
}

run_openapi() {
  step "openapi lint"
  npx --yes @redocly/cli@2.36.0 lint apps/api/internal/spec/openapi.yaml
}

run_sqlc() {
  step "sqlc generate + dirty check"
  (cd "$ROOT" && sqlc generate)
  if ! git diff --quiet -- apps/api/internal/db; then
    echo "sqlc generate left apps/api/internal/db dirty; commit the output" >&2
    git diff --stat -- apps/api/internal/db >&2 || true
    return 1
  fi
}

usage() {
  sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
}

ALL="diff api web rover openapi"

if [ "$#" -eq 0 ]; then
  set -- $ALL
fi

for arg in "$@"; do
  case "$arg" in
    list | -h | --help | help)
      usage
      exit 0
      ;;
    all)
      for s in $ALL; do
        "run_$s"
      done
      ;;
    diff | api | web | rover | openapi | sqlc)
      "run_$arg"
      ;;
    *)
      echo "unknown suite: $arg (try: scripts/verify.sh list)" >&2
      exit 2
      ;;
  esac
done

printf '\nAll requested checks passed.\n'
