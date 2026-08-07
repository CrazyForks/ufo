#!/bin/sh
# Run meaningful local checks (mirrors CONTRIBUTING / CI intent).
#
# Usage:
#   scripts/verify.sh              # all default suites
#   scripts/verify.sh api web      # selected suites
#   scripts/verify.sh rover
#   scripts/verify.sh openapi
#   scripts/verify.sh docs
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

check_diff() {
  step "git diff --check"
  git diff --check
  git diff --cached --check
}

check_docs() {
  step "docs: Markdown prose wraps at 78 source characters"
  node scripts/check-doc-wrap.mjs
}

check_api() {
  scope="${1:-all}"
  step "api: gofmt"
  unformatted="$(gofmt -l apps/api)"
  if [ -n "$unformatted" ]; then
    echo "gofmt needed on:" >&2
    echo "$unformatted" >&2
    return 1
  fi
  [ "$scope" = changes ] && return
  step "api: build / vet / test"
  (
    cd apps/api
    go build ./...
    go vet ./...
    go test ./...
  )
}

check_web() {
  scope="${1:-all}"
  step "web: lint"
  (cd apps/web && npm run lint)
  [ "$scope" = changes ] && return
  if [ "${UFO_CHECK_SKIP_WEB_BUILD:-}" != "1" ]; then
    step "web: build"
    (cd apps/web && npm run build)
  fi
}

check_rover() {
  scope="${1:-all}"
  step "rover: fmt"
  (cd apps/rover && cargo fmt --check)
  [ "$scope" = changes ] && return
  step "rover: clippy / test / build"
  (
    cd apps/rover
    cargo clippy -- -D warnings
    cargo test
    cargo build
  )
}

check_openapi() {
  step "openapi lint"
  npx --yes @redocly/cli@2.46.0 lint apps/api/internal/spec/openapi.yaml
}

check_sqlc() {
  step "sqlc generate + dirty check"
  (cd "$ROOT" && sqlc generate)
  if ! git diff --quiet -- apps/api/internal/db; then
    echo "sqlc generate left apps/api/internal/db dirty; commit the output" >&2
    git diff --stat -- apps/api/internal/db >&2 || true
    return 1
  fi
}

check_changes() {
  check_diff
  check_docs

  changed="$(git diff --cached --name-only --diff-filter=ACMR)"

  case "$changed" in
    *apps/api/*.go*) check_api changes ;;
  esac

  case "$changed" in
    *apps/web/*.ts* | *apps/web/package.json* | *apps/web/tsconfig.json*)
      check_web changes
      ;;
  esac

  case "$changed" in
    *apps/rover/*.rs* | *apps/rover/Cargo.toml* | *apps/rover/Cargo.lock*)
      check_rover changes
      ;;
  esac
}

usage() {
  sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
}

ALL="diff docs api web rover openapi"

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
        "check_$s"
      done
      ;;
    check-changes)
      check_changes
      ;;
    diff | docs | api | web | rover | openapi | sqlc)
      "check_$arg"
      ;;
    *)
      echo "unknown suite: $arg (try: scripts/verify.sh list)" >&2
      exit 2
      ;;
  esac
done

printf '\nAll requested checks passed.\n'
