#!/usr/bin/env bash
# Install the latest released sprawl binary from GitHub.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/ultrakorne/sprawl_cli/master/scripts/install.sh | bash
#
# Env overrides:
#   SPRAWL_VERSION=v0.2.0   # pin a specific tag (default: latest)
#   BIN_DIR=/usr/local/bin  # install location (default: $HOME/.local/bin)
#
# Once installed, future updates can use the built-in `sprawl update`.

set -euo pipefail

REPO="ultrakorne/sprawl_cli"
BIN_NAME="sprawl"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"

err() { printf 'error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || err "missing required tool: $1"; }

need curl
need tar
need uname
need mktemp

# Pick a SHA-256 verifier; refuse to install without one.
if command -v sha256sum >/dev/null 2>&1; then
  sha256_check() { printf '%s  %s\n' "$1" "$2" | sha256sum -c --status -; }
elif command -v shasum >/dev/null 2>&1; then
  sha256_check() { printf '%s  %s\n' "$1" "$2" | shasum -a 256 -c -s -; }
else
  err "neither sha256sum nor shasum found; cannot verify download"
fi

uname_s=$(uname -s)
case "$uname_s" in
  Linux)  os=linux  ;;
  Darwin) os=darwin ;;
  *) err "unsupported OS: $uname_s (only linux and darwin are released)" ;;
esac

uname_m=$(uname -m)
case "$uname_m" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) err "unsupported architecture: $uname_m" ;;
esac

# Resolve tag: SPRAWL_VERSION wins; otherwise hit /releases/latest.
tag="${SPRAWL_VERSION:-}"
if [ -z "$tag" ]; then
  tag=$(curl -fsSL \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' \
    | head -n1 \
    | sed 's/.*"\([^"]*\)"$/\1/')
  [ -n "$tag" ] || err "could not determine latest release tag"
fi
case "$tag" in v*) version=${tag#v} ;; *) version=$tag; tag="v$tag" ;; esac

asset="sprawl_${version}_${os}_${arch}.tar.gz"
base="https://github.com/${REPO}/releases/download/${tag}"

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t sprawl-install)
trap 'rm -rf "$tmp"' EXIT

printf 'downloading %s\n' "$asset"
curl -fsSL -o "$tmp/$asset"          "$base/$asset"
curl -fsSL -o "$tmp/checksums.txt"   "$base/checksums.txt"

want=$(awk -v n="$asset" '{ name=$2; sub(/^\*/, "", name); if (name == n) { print $1; exit } }' "$tmp/checksums.txt")
[ -n "$want" ] || err "checksums.txt has no entry for $asset"
sha256_check "$want" "$tmp/$asset" || err "checksum mismatch for $asset"

tar -xzf "$tmp/$asset" -C "$tmp" "$BIN_NAME"

mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/$BIN_NAME" "$BIN_DIR/$BIN_NAME"

printf 'installed %s %s -> %s/%s\n' "$BIN_NAME" "$tag" "$BIN_DIR" "$BIN_NAME"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) printf 'note: %s is not on PATH; add it to your shell profile.\n' "$BIN_DIR" ;;
esac

printf 'run `%s update` to upgrade in place when new releases ship.\n' "$BIN_NAME"
