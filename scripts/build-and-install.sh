#!/usr/bin/env bash
# Build sprawl_dev and install it into ~/.local/bin. Pass --all to also build
# and install the production sprawl binary.
set -euo pipefail

REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
BUILD_ALL=0

case "${1:-}" in
  "") ;;
  --all) BUILD_ALL=1 ;;
  *)
    echo "usage: $0 [--all]" >&2
    exit 2
    ;;
esac

cd "$REPO_ROOT"
if [[ "$BUILD_ALL" -eq 1 ]]; then
  make build-all
else
  make build-dev
fi

mkdir -p "$BIN_DIR"
install -m 0755 dist/sprawl_dev "$BIN_DIR/sprawl_dev"
if [[ "$BUILD_ALL" -eq 1 ]]; then
  install -m 0755 dist/sprawl "$BIN_DIR/sprawl"
fi

echo "installed:"
echo "  $BIN_DIR/sprawl_dev"
if [[ "$BUILD_ALL" -eq 1 ]]; then
  echo "  $BIN_DIR/sprawl"
fi

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "note: $BIN_DIR is not on PATH" ;;
esac
