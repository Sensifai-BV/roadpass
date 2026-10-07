#!/usr/bin/env bash
# Builds and starts the full stack on the local Docker engine (OrbStack), loads the
# simulated dataset, lets the worker seal it on Fabric, and runs the Playwright
# suite against it. Usage: scripts/e2e.sh [--fresh] [--keep] [--scale 0.3]
#   --fresh  start from empty volumes: first-run setup, new ledger, new vault
set -euo pipefail
cd "$(dirname "$0")/.."
KEEP=0; FRESH=0; SCALE="${SEED_SCALE:-0.3}"
while [ $# -gt 0 ]; do case "$1" in --keep) KEEP=1;; --fresh) FRESH=1;; --scale) SCALE="$2"; shift;; esac; shift; done
DC=(docker compose -f deploy/compose.yml)
log() { printf '\033[1;34m[e2e]\033[0m %s\n' "$*"; }

if [ "$FRESH" = 1 ]; then
  log "removing every volume: a fresh installation"
  "${DC[@]}" --profile debug --profile e2e --profile seed down -v >/dev/null 2>&1 || true
fi
log "building images"
"${DC[@]}" build fabric app
log "starting data services and Fabric"
"${DC[@]}" up -d postgres clickhouse redis fabric mailpit
"${DC[@]}" up -d --wait postgres clickhouse redis
log "first-run setup on an empty installation"
"${DC[@]}" up -d --wait app >/dev/null   # migrates the empty database on start
need=$(curl -fsS http://127.0.0.1:${HTTP_PORT:-8080}/api/v1/setup)
if [ "$need" != '{"needed":true}' ]; then
  [ "$FRESH" = 1 ] && { echo "setup should be needed on a fresh installation, got $need"; exit 1; }
  log "setup check skipped: this installation is already set up (use --fresh)"
else
link=$("${DC[@]}" logs app 2>&1 | grep -o 'http[^" ]*/setup?token=[A-Za-z0-9_-]*' | tail -1)
[ -n "$link" ] || { echo "the app did not log a setup link"; exit 1; }
tok=${link##*token=}
body='{"token":"'"$tok"'","org":{"name":"E2E Platform Operator","country":"NL","city":"Utrecht"},"user":{"name":"Platform Manager","email":"manager@platform.example","password":"a-long-enough-password"}}'
code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -H 'X-Requested-With: roadpass' -d "$body" http://127.0.0.1:${HTTP_PORT:-8080}/api/v1/setup)
[ "$code" = 201 ] || { echo "setup returned $code"; exit 1; }
code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -H 'X-Requested-With: roadpass' -d "$body" http://127.0.0.1:${HTTP_PORT:-8080}/api/v1/setup)
[ "$code" = 403 ] || [ "$code" = 409 ] || { echo "a second setup must be refused, got $code"; exit 1; }
log "setup ok: platform manager created; a second setup is refused"
fi
"${DC[@]}" stop app >/dev/null

log "seeding the simulated chain (scale $SCALE)"
SEED_SCALE="$SCALE" "${DC[@]}" --profile seed run --rm seed
log "waiting for Fabric"
for _ in $(seq 1 120); do
  [ "$(docker inspect -f '{{.State.Health.Status}}' "$("${DC[@]}" ps -q fabric)")" = healthy ] && break; sleep 2
done
log "starting the service and the worker"
"${DC[@]}" up -d --wait app worker
log "running Playwright"
set +e
"${DC[@]}" --profile e2e run --rm e2e
rc=$?
set -e
if [ "$KEEP" = 0 ] && [ $rc = 0 ]; then log "stopping (use --keep to leave it running)"; "${DC[@]}" stop; fi
log "report: e2e/playwright-report/index.html"
exit $rc
