#!/usr/bin/env sh
set -eu

ACTION=${1:-install}
case "$ACTION" in
  install|doctor|uninstall|purge) shift ;;
  *) ACTION=install ;;
esac

SOURCE=${1:-./devcycle}
CHECKSUMS=${2:-}
PREFIX=${PREFIX:-"$HOME/.local"}
DATA_DIR=${DEVCYCLE_DATA_DIR:-"$HOME/.local/share/devcycle"}
SYSTEMD_DIR=${SYSTEMD_DIR:-"$HOME/.config/systemd/user"}
BINARY="$PREFIX/bin/devcycle"
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

verify_checksum() {
  [ -n "$CHECKSUMS" ] || return 0
  [ -f "$CHECKSUMS" ] || { echo "checksum file not found: $CHECKSUMS" >&2; exit 1; }
  name=$(basename "$SOURCE")
  expected=$(awk -v name="$name" '$2 == name || $2 == "*" name { print $1; exit }' "$CHECKSUMS")
  [ -n "$expected" ] || { echo "checksum entry not found for $name" >&2; exit 1; }
  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$SOURCE" | awk '{print $1}')
  else
    actual=$(shasum -a 256 "$SOURCE" | awk '{print $1}')
  fi
  [ "$actual" = "$expected" ] || { echo "SHA-256 verification failed for $SOURCE" >&2; exit 1; }
  echo "verified SHA-256 for $name"
}

has_user_systemd() {
  command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1
}

case "$ACTION" in
  install)
    [ -f "$SOURCE" ] || { echo "binary not found: $SOURCE" >&2; echo "usage: sh scripts/install.sh install [devcycle] [checksums.txt]" >&2; exit 1; }
    verify_checksum
    install -d -m 0755 "$PREFIX/bin"
    install -d -m 0700 "$DATA_DIR"
    install -m 0755 "$SOURCE" "$BINARY"
    unit="$SCRIPT_DIR/../deploy/devcycle.service"
    if [ -f "$unit" ]; then
      install -d -m 0755 "$SYSTEMD_DIR"
      install -m 0644 "$unit" "$SYSTEMD_DIR/devcycle.service"
      has_user_systemd && systemctl --user daemon-reload || true
      if [ "${ENABLE_SERVICE:-0}" = 1 ] && has_user_systemd; then
        systemctl --user enable --now devcycle.service
      fi
    fi
    "$BINARY" version
    echo "installed $BINARY"
    echo "data directory: $DATA_DIR"
    echo "run: $BINARY serve --db $DATA_DIR/devcycle.db"
    ;;
  doctor)
    [ -x "$BINARY" ] || { echo "devcycle is not installed at $BINARY" >&2; exit 1; }
    [ -d "$DATA_DIR" ] && [ -w "$DATA_DIR" ] || { echo "data directory is unavailable or not writable: $DATA_DIR" >&2; exit 1; }
    command -v git >/dev/null 2>&1 || { echo "git is required but was not found" >&2; exit 1; }
    "$BINARY" version
    echo "doctor: binary, Git, and data directory are ready"
    ;;
  uninstall)
    if has_user_systemd; then
      systemctl --user disable --now devcycle.service >/dev/null 2>&1 || true
    fi
    rm -f "$SYSTEMD_DIR/devcycle.service" "$BINARY"
    has_user_systemd && systemctl --user daemon-reload || true
    echo "removed DevCycle binaries; preserved $DATA_DIR"
    ;;
  purge)
    case "$DATA_DIR" in
      */devcycle|*/devcycle/*) ;;
      *) echo "refusing to purge unexpected data directory: $DATA_DIR" >&2; exit 1 ;;
    esac
    [ "${DEVCYCLE_CONFIRM_PURGE:-}" = "$DATA_DIR" ] || { echo "set DEVCYCLE_CONFIRM_PURGE to the exact data directory" >&2; exit 1; }
    rm -rf "$DATA_DIR"
    echo "purged $DATA_DIR"
    ;;
esac
