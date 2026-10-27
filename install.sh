#!/bin/sh
# Installs the Judoscale CLI (judo) from a GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/judoscale/judoscale-cli/main/install.sh | sh
#
# JUDO_VERSION      version to install, e.g. 0.1.0 (default: latest)
# JUDO_INSTALL_DIR  where to put the binary (default: /usr/local/bin if writable, else ~/.local/bin)
set -eu

repo=judoscale/judoscale-cli

fail() {
  echo "install.sh: $*" >&2
  exit 1
}

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "unsupported OS $(uname -s); download a release from https://github.com/$repo/releases" ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) fail "unsupported architecture $(uname -m)" ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  fail "sha256sum or shasum is required to verify the download"
fi

version=${JUDO_VERSION:-}
version=${version#v}
if [ -z "$version" ]; then
  # The redirect avoids the API and its rate limits.
  url=$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
    fail "could not determine the latest release"
  version=${url##*/v}
fi

if [ -n "${JUDO_INSTALL_DIR:-}" ]; then
  dir=$JUDO_INSTALL_DIR
elif [ -w /usr/local/bin ]; then
  dir=/usr/local/bin
else
  dir=$HOME/.local/bin
fi

archive=judo_${version}_${os}_${arch}.tar.gz
base=https://github.com/$repo/releases/download/v$version

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Installing judo $version ($os/$arch) to $dir"

curl -fsSL -o "$tmp/$archive" "$base/$archive" ||
  fail "could not download $base/$archive (is v$version a release with binaries attached?)"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" ||
  fail "could not download $base/checksums.txt"

expected=$(grep "  $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)
[ -n "$expected" ] || fail "$archive is not listed in checksums.txt"
[ "$(sha256 "$tmp/$archive")" = "$expected" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" judo
mkdir -p "$dir"
install -m 755 "$tmp/judo" "$dir/judo"
ln -sf judo "$dir/judoscale"

echo "Installed $("$dir/judo" version)"

case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Add $dir to your PATH to run judo." ;;
esac
