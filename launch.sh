#!/usr/bin/env bash
# launch.sh — start Ollama (if needed), the Go backend in DEV_MODE, and the
# Vite dev server. Always runs in dev mode (no Firebase required).
#
# Ctrl-C stops everything cleanly.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$REPO_ROOT"

OLLAMA_MODEL="${OLLAMA_MODEL:-phi3}"

log()  { printf '\033[1;34m[launch]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[launch]\033[0m %s\n' "$*" >&2; }
err()  { printf '\033[1;31m[launch]\033[0m %s\n' "$*" >&2; exit 1; }

command -v ollama >/dev/null 2>&1 || err "Ollama not installed. Run ./install.sh first."
command -v go     >/dev/null 2>&1 || err "Go not installed. Run ./install.sh first."
command -v npm    >/dev/null 2>&1 || err "npm not installed. Run ./install.sh first."

OLLAMA_PID=""
BACKEND_PID=""
FRONTEND_PID=""

cleanup() {
  log "Shutting down..."
  for pid in "$FRONTEND_PID" "$BACKEND_PID" "$OLLAMA_PID"; do
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  done
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# --- Ollama -----------------------------------------------------------------

if curl -fsS http://localhost:11434/api/tags >/dev/null 2>&1; then
  log "Ollama already running."
else
  log "Starting Ollama daemon..."
  ollama serve >/tmp/ollama.log 2>&1 &
  OLLAMA_PID=$!
  for _ in $(seq 1 20); do
    sleep 1
    if curl -fsS http://localhost:11434/api/tags >/dev/null 2>&1; then break; fi
  done
  curl -fsS http://localhost:11434/api/tags >/dev/null 2>&1 \
    || err "Ollama failed to start within 20s — see /tmp/ollama.log"
fi

# Make sure the model is present (cheap no-op if it already is).
if ! ollama list 2>/dev/null | awk '{print $1}' | grep -q "^${OLLAMA_MODEL}\(:.*\)\?$"; then
  log "Model '$OLLAMA_MODEL' missing — pulling now."
  ollama pull "$OLLAMA_MODEL"
fi

# --- backend (always DEV_MODE=1) --------------------------------------------

log "Starting backend on http://localhost:8080 (DEV_MODE=1)..."
( cd backend && DEV_MODE=1 OLLAMA_MODEL="$OLLAMA_MODEL" go run ./cmd/httpserver ) &
BACKEND_PID=$!

# --- frontend ---------------------------------------------------------------

if [ ! -f frontend/.env.local ]; then
  warn "frontend/.env.local not found — copying from .env.example with VITE_DEV_MODE=1."
  cp frontend/.env.example frontend/.env.local
  sed -i.bak 's/^VITE_DEV_MODE=0/VITE_DEV_MODE=1/' frontend/.env.local
  rm -f frontend/.env.local.bak
fi

log "Starting frontend on http://localhost:5173..."
( cd frontend && npm run dev ) &
FRONTEND_PID=$!

log "All services up. Open http://localhost:5173 — Ctrl-C to stop."
wait "$BACKEND_PID" "$FRONTEND_PID"
