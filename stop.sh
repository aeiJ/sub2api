#!/usr/bin/env zsh
set -euo pipefail

SESSION_NAME="sub2api"
ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
SERVER_BIN="$ROOT_DIR/backend/bin/server"

usage() {
  cat <<EOF
Usage:
  cd "$ROOT_DIR"
  ./stop.sh

Stops the detached screen session named "$SESSION_NAME" and any Sub2API process
from this project's backend directory that is still listening on port 8080.

Useful commands:
  ./start.sh      Start Sub2API
  ./restart.sh    Restart Sub2API
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

screen_has_session() {
  local sessions
  sessions="$(screen -ls 2>/dev/null || true)"
  grep -q "[.]${SESSION_NAME}[[:space:]]" <<<"$sessions"
}

server_pids() {
  local matched=""

  matched="$(pgrep -f "$SERVER_BIN" 2>/dev/null || true)"

  local port_pids pid cwd
  port_pids="$(lsof -nP -tiTCP:8080 -sTCP:LISTEN 2>/dev/null || true)"
  for pid in ${(f)port_pids}; do
    cwd="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -1)"
    if [[ "$cwd" == "$BACKEND_DIR" ]]; then
      matched+="${matched:+$'\n'}$pid"
    fi
  done

  if [[ -n "$matched" ]]; then
    print -r -- "$matched" | sort -u
  fi
}

if ! command -v screen >/dev/null 2>&1; then
  echo "screen is required but was not found."
  exit 1
fi

if ! screen_has_session; then
  pids="$(server_pids)"
  if [[ -n "$pids" ]]; then
    echo "Found Sub2API process without screen session; stopping it."
    kill $pids >/dev/null 2>&1 || true
  else
    echo "Sub2API is not running."
    exit 0
  fi
else
  screen -S "$SESSION_NAME" -X quit
fi

for _ in {1..10}; do
  if ! screen_has_session && [[ -z "$(server_pids)" ]]; then
    echo "Sub2API stopped."
    exit 0
  fi
  sleep 1
done

pids="$(server_pids)"
if [[ -n "$pids" ]]; then
  kill -TERM $pids >/dev/null 2>&1 || true
  sleep 1
fi

if ! screen_has_session && [[ -z "$(server_pids)" ]]; then
  echo "Sub2API stopped."
  exit 0
fi

echo "Timed out waiting for Sub2API to stop."
exit 1
