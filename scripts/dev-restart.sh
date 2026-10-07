#!/usr/bin/env bash
# Rebuild the dashboard and the binary, then restart the local server (DEMO_MODE).
set -euo pipefail
cd "$(dirname "$0")/.."
(cd web && npx tsc -b && npx vite build >/dev/null)
go build -o bin/roadpass ./cmd/roadpass
pkill -f "bin/roadpass serve" 2>/dev/null || true
mkdir -p .run
DEMO_MODE=1 HTTP_ADDR=127.0.0.1:8090 PUBLIC_URL=http://127.0.0.1:8090 nohup ./bin/roadpass serve --worker > .run/serve.log 2>&1 &
for _ in $(seq 1 40); do curl -fs http://127.0.0.1:8090/healthz >/dev/null && break; sleep 0.25; done
echo "roadpass on http://127.0.0.1:8090"
