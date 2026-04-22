#!/usr/bin/env bash
# Build both sprawl binaries and install them into ~/.local/bin.
set -euo pipefail

REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"

cd "$REPO_ROOT"
make build-all

mkdir -p "$BIN_DIR"
install -m 0755 dist/sprawl     "$BIN_DIR/sprawl"
install -m 0755 dist/sprawl_dev "$BIN_DIR/sprawl_dev"

echo "installed:"
echo "  $BIN_DIR/sprawl"
echo "  $BIN_DIR/sprawl_dev"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "note: $BIN_DIR is not on PATH" ;;
esac
