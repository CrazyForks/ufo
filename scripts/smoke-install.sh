#!/bin/sh
set -eu

ROOT="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

case "$(uname -m)" in
  x86_64|amd64) arch="x86_64" ;;
  arm64|aarch64) arch="aarch64" ;;
  *) echo "ufo smoke install: unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac
case "$(uname -s)" in
  Darwin) target="${arch}-apple-darwin" ;;
  FreeBSD) target="${arch}-unknown-freebsd" ;;
  Linux)
    if [ -e "/lib/ld-musl-${arch}.so.1" ] || (command -v ldd >/dev/null 2>&1 && ldd --version 2>&1 | grep -qi musl); then
      target="${arch}-unknown-linux-musl"
    else
      target="${arch}-unknown-linux-gnu"
    fi
    ;;
  *) echo "ufo smoke install: unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac
asset="ufo-${target}.tar.gz"
release="$tmp/release"
package="$tmp/package"
install_dir="$tmp/install"

mkdir -p "$release" "$package" "$install_dir"
cargo build --manifest-path "$ROOT/apps/rover/Cargo.toml" --locked
cp "$ROOT/apps/rover/target/debug/ufo" "$package/ufo"
tar -C "$package" -czf "$release/$asset" .

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$release" && sha256sum "$asset" > "$asset.sha256")
else
  (cd "$release" && shasum -a 256 "$asset" > "$asset.sha256")
fi

UFO_ROVER_RELEASE_BASE_URL="file://$release" UFO_ROVER_INSTALL_DIR="$install_dir" "$ROOT/scripts/install.sh"

"$install_dir/ufo" --help >/dev/null
"$install_dir/ufo" rover --help >/dev/null

echo "ufo install smoke passed"
