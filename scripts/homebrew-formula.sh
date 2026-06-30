#!/bin/sh
set -eu

usage() {
  echo "usage: scripts/homebrew-formula.sh vX.Y.Z [dist-dir]" >&2
  exit 2
}

[ "${1:-}" ] || usage

tag="$1"
dist="${2:-dist}"
case "$tag" in
  v*) ;;
  *) tag="v$tag" ;;
esac
version="${tag#v}"
base="https://github.com/fengsi/ufo/releases/download/${tag}"

sha_for() {
  file="${dist}/ufo-$1.tar.gz.sha256"
  if [ ! -f "$file" ]; then
    echo "missing checksum: $file" >&2
    exit 1
  fi
  awk '{print $1; exit}' "$file"
}

cat <<FORMULA
class UfoCli < Formula
  desc "Local rover CLI for UFO"
  homepage "https://getufo.dev"
  version "${version}"
  license "BSD-3-Clause"

  on_macos do
    on_arm do
      url "${base}/ufo-aarch64-apple-darwin.tar.gz"
      sha256 "$(sha_for aarch64-apple-darwin)"
    end
    on_intel do
      url "${base}/ufo-x86_64-apple-darwin.tar.gz"
      sha256 "$(sha_for x86_64-apple-darwin)"
    end
  end

  on_linux do
    on_arm do
      url "${base}/ufo-aarch64-unknown-linux-gnu.tar.gz"
      sha256 "$(sha_for aarch64-unknown-linux-gnu)"
    end
    on_intel do
      url "${base}/ufo-x86_64-unknown-linux-gnu.tar.gz"
      sha256 "$(sha_for x86_64-unknown-linux-gnu)"
    end
  end

  def install
    bin.install "ufo"
  end

  test do
    assert_match "UFO local rover", shell_output("#{bin}/ufo --help")
    assert_match "Rover controls", shell_output("#{bin}/ufo rover --help")
  end
end
FORMULA
