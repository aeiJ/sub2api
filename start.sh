#!/usr/bin/env zsh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
SESSION_NAME="sub2api"
LOG_FILE="$BACKEND_DIR/data/sub2api.screen.log"
HEALTH_URL="http://localhost:8080/health"
SERVER_BIN="$BACKEND_DIR/bin/server"

usage() {
  cat <<EOF
Usage:
  cd "$ROOT_DIR"
  ./start.sh

Starts Sub2API in a detached screen session named "$SESSION_NAME".

Useful commands:
  ./stop.sh       Stop Sub2API
  ./restart.sh    Restart Sub2API
  screen -r $SESSION_NAME
  tail -f "$LOG_FILE"
EOF
}

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

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if ! command -v screen >/dev/null 2>&1; then
  echo "screen is required but was not found."
  exit 1
fi

if [[ ! -x "$SERVER_BIN" ]]; then
  echo "Backend binary not found: $SERVER_BIN"
  echo "Build it first, then run this script again."
  exit 1
fi

mkdir -p "$BACKEND_DIR/data"

if curl -fsS --max-time 5 "$HEALTH_URL" >/dev/null 2>&1; then
  if screen_has_session || [[ -n "$(server_pids)" ]]; then
    echo "Sub2API is already running: http://localhost:8080"
    echo "Project: $ROOT_DIR"
    echo "Log: $LOG_FILE"
    echo "Stop: $ROOT_DIR/stop.sh"
    echo "Restart: $ROOT_DIR/restart.sh"
    exit 0
  fi
fi

if screen_has_session; then
  if curl -fsS --max-time 5 "$HEALTH_URL" >/dev/null 2>&1; then
    echo "Sub2API is already running: $HEALTH_URL"
    exit 0
  fi

  echo "Found existing screen session '$SESSION_NAME' but health check failed; restarting it."
  screen -S "$SESSION_NAME" -X quit >/dev/null 2>&1 || true
  sleep 1
fi

screen -dmS "$SESSION_NAME" zsh -lc "cd '$BACKEND_DIR' && DATA_DIR=./data ./bin/server >> '$LOG_FILE' 2>&1"

for _ in {1..30}; do
  if curl -fsS --max-time 2 "$HEALTH_URL" >/dev/null 2>&1; then
    echo "Sub2API started: http://localhost:8080"
    echo "Project: $ROOT_DIR"
    echo "Log: $LOG_FILE"
    echo "Stop: $ROOT_DIR/stop.sh"
    echo "Restart: $ROOT_DIR/restart.sh"
    exit 0
  fi
  sleep 1
done

echo "Sub2API did not become healthy within 30 seconds."
echo "Log: $LOG_FILE"
tail -40 "$LOG_FILE" 2>/dev/null || true
exit 1
