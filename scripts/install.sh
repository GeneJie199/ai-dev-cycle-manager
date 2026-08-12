#!/usr/bin/env sh
set -eu

SOURCE=${1:-./devcycle}
PREFIX=${PREFIX:-"$HOME/.local"}
DATA_DIR=${DEVCYCLE_DATA_DIR:-"$HOME/.local/share/devcycle"}

if [ ! -f "$SOURCE" ]; then
  echo "binary not found: $SOURCE" >&2
  echo "usage: ./scripts/install.sh [path-to-devcycle]" >&2
  exit 1
fi

install -d -m 0755 "$PREFIX/bin" "$DATA_DIR"
install -m 0755 "$SOURCE" "$PREFIX/bin/devcycle"
echo "installed $PREFIX/bin/devcycle"
echo "data directory: $DATA_DIR"
echo "run: $PREFIX/bin/devcycle serve --db $DATA_DIR/devcycle.db"
