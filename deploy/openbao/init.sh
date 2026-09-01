#!/bin/sh
# Local/E2E only: initialise OpenBao once (1 unseal share, kept in a volume),
# unseal it on every start, enable Transit and issue ROADPASS a token that can
# create, read, rotate and sign with keys, and nothing else: no export, no delete.
set -eu
export BAO_ADDR=${BAO_ADDR:-http://openbao:8200}
STATE=/state
mkdir -p "$STATE"

status() { bao status -format=json 2>/dev/null || true; }
for _ in $(seq 1 60); do
  wget -q -O /dev/null "$BAO_ADDR/v1/sys/seal-status" 2>/dev/null && break
  sleep 1
done

if status | grep -q '"initialized": false'; then
  echo "initialising OpenBao"
  bao operator init -key-shares=1 -key-threshold=1 -format=json > "$STATE/init.json"
  chmod 600 "$STATE/init.json"
fi
UNSEAL=$(sed -n 's/.*"unseal_keys_b64": *\[ *"\([^"]*\)".*/\1/p' "$STATE/init.json" | head -1)
if [ -z "$UNSEAL" ]; then
  UNSEAL=$(tr -d '\n ' < "$STATE/init.json" | sed -n 's/.*"unseal_keys_b64":\["\([^"]*\)".*/\1/p')
fi
ROOT=$(tr -d '\n ' < "$STATE/init.json" | sed -n 's/.*"root_token":"\([^"]*\)".*/\1/p')
if status | grep -q '"sealed": true'; then
  echo "unsealing OpenBao"
  bao operator unseal "$UNSEAL" >/dev/null
fi
export BAO_TOKEN="$ROOT"

bao secrets list -format=json | grep -q '"transit/"' || bao secrets enable transit
bao secrets list -format=json | grep -q '"roadpass-kv/"' || bao secrets enable -path=roadpass-kv -version=2 kv
bao policy write roadpass - <<'POLICY'
path "transit/keys/*"        { capabilities = ["create", "read", "update"] }
path "transit/keys/+/rotate" { capabilities = ["update"] }
path "transit/sign/*"        { capabilities = ["update"] }
path "transit/verify/*"      { capabilities = ["update"] }
# Channel and platform secrets the service reads back (SMTP, Twilio, webhooks).
path "roadpass-kv/data/*"     { capabilities = ["create", "read", "update"] }
path "roadpass-kv/metadata/*" { capabilities = ["delete"] }
# Never: make a key exportable or deletable, export it, or back it up.
path "transit/keys/+/config" { capabilities = ["deny"] }
path "transit/export/*"      { capabilities = ["deny"] }
path "transit/backup/*"      { capabilities = ["deny"] }
POLICY

if [ ! -s /token/roadpass.token ] || ! BAO_TOKEN="$(cat /token/roadpass.token)" bao token lookup >/dev/null 2>&1; then
  echo "issuing the ROADPASS token"
  bao token create -policy=roadpass -period=768h -orphan -display-name=roadpass -format=json \
    | tr -d '\n ' | sed -n 's/.*"client_token":"\([^"]*\)".*/\1/p' > /token/roadpass.token
  chmod 644 /token/roadpass.token
fi
echo "OpenBao ready"
