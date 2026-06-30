#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NATIVE_DIR="$ROOT_DIR/native"
LANCEDB_MODULE="github.com/lancedb/lancedb-go"

cd "$ROOT_DIR"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

platform_arch() {
  local os
  local arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m | tr '[:upper:]' '[:lower:]')"

  case "$os" in
    darwin)
      os="darwin"
      ;;
    linux)
      os="linux"
      ;;
    mingw*|msys*|cygwin*)
      os="windows"
      ;;
    *)
      echo "unsupported operating system: $os" >&2
      exit 1
      ;;
  esac

  case "$arch" in
    x86_64|amd64)
      arch="amd64"
      ;;
    arm64|aarch64)
      arch="arm64"
      ;;
    *)
      echo "unsupported architecture: $arch" >&2
      exit 1
      ;;
  esac

  echo "${os}_${arch}"
}

require_command go
require_command uname

target="$(platform_arch)"
mkdir -p "$NATIVE_DIR"

echo "Downloading Go modules declared in go.mod..."
go mod download

lancedb_dir="$(go list -m -f '{{.Dir}}' "$LANCEDB_MODULE")"
lancedb_version="$(go list -m -f '{{.Version}}' "$LANCEDB_MODULE")"
installer="$lancedb_dir/scripts/download-artifacts.sh"

if [[ ! -f "$installer" ]]; then
  echo "expected LanceDB artifact installer was not found: $installer" >&2
  exit 1
fi

echo "Installing LanceDB native artifacts for $target from $LANCEDB_MODULE@$lancedb_version..."
(
  cd "$NATIVE_DIR"
  bash "$installer" "$lancedb_version"
)

if [[ -f "$NATIVE_DIR/RELEASE_NOTES.md" ]]; then
  rm -f "$NATIVE_DIR/RELEASE_NOTES.md"
fi

lib_file="$NATIVE_DIR/lib/$target/liblancedb_go.a"
header_file="$NATIVE_DIR/include/lancedb.h"

if [[ ! -f "$lib_file" ]]; then
  echo "expected native library was not found: $lib_file" >&2
  exit 1
fi

if [[ ! -f "$header_file" ]]; then
  echo "expected LanceDB header was not found: $header_file" >&2
  exit 1
fi

echo "LanceDB Go artifacts installed for $target."
echo
echo "Smoke check:"
echo "  go run -tags lancedb_smoke ./lancedb_smoke.go"
