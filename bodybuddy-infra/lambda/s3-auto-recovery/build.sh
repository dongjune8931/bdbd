#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST_DIR="$ROOT_DIR/dist"

mkdir -p "$DIST_DIR"

(
  cd "$ROOT_DIR"
  GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$DIST_DIR/bootstrap" .
  (cd "$DIST_DIR" && zip -q -j bootstrap.zip bootstrap)
)

echo "$DIST_DIR/bootstrap.zip"
