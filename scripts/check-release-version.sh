#!/bin/sh
set -eu

want="${1:-}"
if [ -z "$want" ]; then
  echo "usage: scripts/check-release-version.sh X.Y.Z" >&2
  exit 2
fi
case "$want" in
  v*) want="${want#v}" ;;
esac

check() {
  name="$1"
  got="$2"
  if [ "$got" != "$want" ]; then
    echo "$name version is $got, want $want" >&2
    exit 1
  fi
}

check "rover Cargo.toml" "$(awk -F '"' '/^version = / { print $2; exit }' apps/rover/Cargo.toml)"
check "web package.json" "$(awk -F '"' '/"version":/ { print $4; exit }' apps/web/package.json)"
check "OpenAPI" "$(awk '/^  version: / { print $2; exit }' apps/api/internal/spec/openapi.yaml)"
check "Hub rover gate" "$(sed -n 's/.*currentRoverVersion[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' apps/api/internal/server/server.go | head -1)"

echo "release version ok: $want"
