#!/usr/bin/env bash
# Smoke test of the write paths against a running server with DEMO_MODE=1:
# API key, JSON (with idempotency and validation), NGSI-LD, CSV, receipt.
set -euo pipefail
B="${1:-http://127.0.0.1:8090}"
J="$(mktemp)"; trap 'rm -f "$J" /tmp/rp-smoke.csv' EXIT
H=(-H 'Content-Type: application/json' -H 'X-Requested-With: roadpass')
ok()   { printf '\033[32m✓\033[0m %s\n' "$*"; }
bad()  { printf '\033[31m✗ %s\033[0m\n' "$*"; exit 1; }
want() { [ "$1" = "$2" ] && ok "$3 ($1)" || bad "$3: got $1, want $2"; }

user=$(curl -s "$B/api/v1/auth/demo-users" | python3 -c "import sys,json; print([x['userId'] for x in json.load(sys.stdin) if x['orgId']=='org_sel-pinto' and x['role']=='admin'][0])")
curl -s -c "$J" "${H[@]}" -d "{\"userId\":\"$user\"}" "$B/api/v1/auth/demo-login" >/dev/null
KEY=$(curl -s -b "$J" "${H[@]}" -d '{"name":"smoke","scopes":["records:read","records:write"]}' "$B/api/v1/org/api-keys" | python3 -c "import sys,json; print(json.load(sys.stdin)['secret'])")
A=(-H "Authorization: Bearer $KEY")
ok "API key issued"
ref="SMOKE-$(date +%s)"
body="{\"records\":[{\"type\":\"reject_lot\",\"ref\":\"$ref\",\"siteId\":\"site_sel-pinto_1\",\"occurredAt\":\"2026-10-14T09:00:00Z\",\"massKg\":18450,\"data\":{\"source\":\"municipal_packaging\",\"ldpe_share_pct\":76,\"avoided_route\":\"landfill\",\"moisture_pct\":12,\"contamination_pct\":11}}]}"
code=$(curl -s -o /tmp/rp-smoke.json -w '%{http_code}' "${A[@]}" -H 'Content-Type: application/json' -H "Idempotency-Key: $ref" -d "$body" "$B/api/v1/records")
want "$code" 201 "JSON record stored"
id=$(python3 -c "import json; r=json.load(open('/tmp/rp-smoke.json'))['results'][0]; print(r['id'])"); ok "issued $id"
code=$(curl -s -o /dev/null -w '%{http_code}' "${A[@]}" -H 'Content-Type: application/json' -H "Idempotency-Key: $ref" -d "$body" "$B/api/v1/records")
want "$code" 200 "idempotent replay"
code=$(curl -s -o /dev/null -w '%{http_code}' "${A[@]}" -H 'Content-Type: application/json' -d '{"records":[{"type":"asphalt_lot","ref":"X","occurredAt":"2026-10-14T09:00:00Z","massKg":1,"data":{}}]}' "$B/api/v1/records")
want "$code" 422 "invalid row rejected"
code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer rpk_test_nope" "$B/api/v1/records")
want "$code" 401 "unknown key refused"
loc=$(curl -s -o /dev/null -w '%{http_code} %header{location}' "${A[@]}" -H 'Content-Type: application/ld+json' -d "{\"@context\":\"$B/ngsi-ld/v1/context.jsonld\",\"type\":\"PlasticRejectLot\",\"reference\":\"N$ref\",\"dateObserved\":\"2026-10-14T10:00:00Z\",\"mass\":{\"type\":\"Property\",\"value\":17.2,\"unitCode\":\"TNE\"},\"atSite\":{\"type\":\"Relationship\",\"object\":\"urn:ngsi-ld:Site:site_sel-pinto_1\"},\"source\":\"commercial_film\",\"ldpeSharePct\":81,\"avoidedRoute\":\"incineration\",\"moisturePct\":9,\"contaminationPct\":10}" "$B/ngsi-ld/v1/entities")
want "${loc%% *}" 201 "NGSI-LD entity created at ${loc#* }"
code=$(curl -s -o /dev/null -w '%{http_code}' "${A[@]}" -H 'Content-Type: application/ld+json' -d '{"@context":"https://example.org/x.jsonld","type":"PlasticRejectLot"}' "$B/ngsi-ld/v1/entities")
want "$code" 503 "foreign @context refused (LdContextNotAvailable)"
printf 'ref;site_id;occurred_at;mass_kg;source;ldpe_share_pct;avoided_route;moisture_pct;contamination_pct\nC1%s;site_sel-pinto_1;2026-10-14 11:30;19100;municipal_packaging;72,5;landfill;14;12\nC2%s;site_sel-pinto_1;2026-10-14 15:45;20300;municipal_packaging;70;landfill;13;13\n' "$ref" "$ref" > /tmp/rp-smoke.csv
valid=$(curl -s "${A[@]}" -F file=@/tmp/rp-smoke.csv "$B/api/v1/imports?type=reject_lot&dryRun=1" | python3 -c "import sys,json; print(json.load(sys.stdin)['valid'])")
want "$valid" True "CSV dry run (semicolons, decimal comma, Madrid time)"
code=$(curl -s -o /dev/null -w '%{http_code}' "${A[@]}" -F file=@/tmp/rp-smoke.csv "$B/api/v1/imports?type=reject_lot")
want "$code" 201 "CSV imported"
code=$(curl -s -o /tmp/rp-smoke.json -w '%{http_code}' "$B/api/v1/public/passports/$id")
want "$code" 200 "public passport of $id"
code=$(curl -s -o /dev/null -w '%{http_code}' "${A[@]}" -H 'Content-Type: application/json' -d '{"readings":[{"ts":"2026-10-14T09:00:00Z","siteId":"site_sel-pinto_1","line":"L1","meltC":205,"pressureBar":160,"throughputKgH":900,"rpm":85,"loadPct":70}]}' "$B/api/v1/telemetry")
want "$code" 202 "telemetry queued"
echo "all smoke checks passed"
