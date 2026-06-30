#!/bin/sh
set -eu

repo="${UFO_ROVER_RELEASE_REPO:-fengsi/ufo}"
version="${UFO_ROVER_VERSION:-latest}"
install_dir="${UFO_ROVER_INSTALL_DIR:-$HOME/.local/bin}"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "ufo install: missing required command: $1" >&2
    exit 1
  }
}

need curl
need tar

case "$(uname -m)" in
  x86_64|amd64) arch="x86_64" ;;
  arm64|aarch64) arch="aarch64" ;;
  *)
    echo "ufo install: unsupported CPU: $(uname -m)" >&2
    exit 1
    ;;
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
  *)
    echo "ufo install: unsupported OS: $(uname -s)" >&2
    exit 1
    ;;
esac

case "$target" in
  aarch64-apple-darwin|x86_64-apple-darwin|x86_64-unknown-freebsd|aarch64-unknown-linux-gnu|x86_64-unknown-linux-gnu|aarch64-unknown-linux-musl|x86_64-unknown-linux-musl) ;;
  *)
    echo "ufo install: no prebuilt rover for $target; use: cargo install ufo-cli" >&2
    exit 1
    ;;
esac

asset="ufo-${target}.tar.gz"
if [ "$version" = "latest" ]; then
  base_url="${UFO_ROVER_RELEASE_BASE_URL:-https://github.com/${repo}/releases/latest/download}"
else
  case "$version" in
    v*) tag="$version" ;;
    *) tag="v$version" ;;
  esac
  base_url="${UFO_ROVER_RELEASE_BASE_URL:-https://github.com/${repo}/releases/download/${tag}}"
fi
base_url="${base_url%/}"

tmp="$(mktemp -d)"
install_tmp=""
trap 'rm -rf "$tmp"; [ -z "$install_tmp" ] || rm -f "$install_tmp"' EXIT INT TERM

curl -fsSL "${base_url}/${asset}" -o "${tmp}/${asset}"
curl -fsSL "${base_url}/${asset}.sha256" -o "${tmp}/${asset}.sha256"

(
  cd "$tmp"
  expected="$(awk '{print $1; exit}' "${asset}.sha256")"
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s  %s\n' "$expected" "$asset" | sha256sum -c -
  else
    if command -v shasum >/dev/null 2>&1; then
      printf '%s  %s\n' "$expected" "$asset" | shasum -a 256 -c -
    elif command -v sha256 >/dev/null 2>&1; then
      got="$(sha256 -q "$asset")"
      [ "$got" = "$expected" ] || {
        echo "ufo install: checksum mismatch for $asset" >&2
        exit 1
      }
    else
      echo "ufo install: missing required command: sha256sum, shasum, or sha256" >&2
      exit 1
    fi
  fi
)

tar -xzf "${tmp}/${asset}" -C "$tmp"
mkdir -p "$install_dir"
install_tmp="${install_dir}/.ufo.$$"
cp "${tmp}/ufo" "$install_tmp"
chmod 0755 "$install_tmp"
mv -f "$install_tmp" "${install_dir}/ufo"

"${install_dir}/ufo" --help >/dev/null
"${install_dir}/ufo" rover --help >/dev/null

echo "ufo installed to ${install_dir}/ufo"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "Add ${install_dir} to PATH before running ufo." ;;
esac
