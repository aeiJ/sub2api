#!/usr/bin/env zsh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"

usage() {
  cat <<EOF
Usage:
  cd "$ROOT_DIR"
  ./restart.sh

Restarts Sub2API by running:
  ./stop.sh
  ./start.sh
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

"$ROOT_DIR/stop.sh"
"$ROOT_DIR/start.sh"
