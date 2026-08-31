#!/usr/bin/env bash
# ROADPASS local Hyperledger Fabric network — native processes, no Docker.
#
#   ./network.sh install   download Fabric binaries (once)
#   ./network.sh up        crypto + channels + orderer + 2 peers
#   ./network.sh deploy    build + install + approve + commit roadpass-proof (chaincode-as-a-service)
#   ./network.sh status    show processes and anchored-object counts
#   ./network.sh down      stop processes (ledger data kept)
#   ./network.sh restart   down + start processes on existing data (backup/restore drill)
#   ./network.sh backup    stop, archive ledgers + crypto + config to backups/, restart
#   ./network.sh restore F stop, wipe, restore archive F, restart (backup/restore drill)
#   ./network.sh onboard O add actor org O (MSP + client identity) to channel roadpass
#   ./network.sh clean     down + delete all generated state
#
# Works on macOS (arm64/amd64) and Linux. Needs: bash, curl, tar, Go >= 1.24.
set -euo pipefail

FABRIC_VERSION="${FABRIC_VERSION:-2.5.16}"
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
FAB="${FABRIC_HOME:-$HERE/.fabric}"            # binaries, builders, default config
RUN="$HERE/.run"               # pids, logs, generated config, chaincode build
DATA="$HERE/data"              # ledgers
CRYPTO="$HERE/crypto"
ART="$HERE/artifacts"
CC_NAME="roadpass-proof"
CC_VERSION="2.1"
CC_SEQUENCE=1
CC_ADDR="127.0.0.1:9999"
CHANNELS="roadpass sandbox"
export PATH="$FAB/bin:$PATH"
export COPYFILE_DISABLE=1   # macOS tar: no AppleDouble ._* entries in packages and backups

ORDERER_CA="$CRYPTO/ordererOrganizations/roadpass.local/orderers/orderer.roadpass.local/tls/ca.crt"

log()  { printf '\033[1;34m[network]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[network] %s\033[0m\n' "$*" >&2; exit 1; }

# ------------------------------------------------------------------ install
cmd_install() {
  if [ -x "$FAB/bin/peer" ] && "$FAB/bin/peer" version 2>/dev/null | grep -q "Version: v\?$FABRIC_VERSION"; then
    log "Fabric $FABRIC_VERSION already installed in $FAB"; return
  fi
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"; case "$arch" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; esac
  local url="https://github.com/hyperledger/fabric/releases/download/v${FABRIC_VERSION}/hyperledger-fabric-${os}-${arch}-${FABRIC_VERSION}.tar.gz"
  log "downloading $url"
  mkdir -p "$FAB"
  curl -fsSL "$url" | tar -xz -C "$FAB"
  if [ "$os" = darwin ]; then xattr -dr com.apple.quarantine "$FAB" 2>/dev/null || true; fi
  "$FAB/bin/peer" version | head -3
}

# ------------------------------------------------------------------ helpers
org_env() { # org_env <sensifai|plasmix> [admin|user]
  local org="$1" who="${2:-admin}" port msp user
  case "$org" in
    sensifai) port=7051; msp=SensifaiMSP;;
    plasmix)  port=9051; msp=PlasmixMSP;;
    *) die "unknown org $org";;
  esac
  [ "$who" = admin ] && user=Admin || user=User1
  local base="$CRYPTO/peerOrganizations/$org.roadpass.local"
  export FABRIC_CFG_PATH="$RUN/config"
  export CORE_PEER_TLS_ENABLED=true
  export CORE_PEER_LOCALMSPID="$msp"
  export CORE_PEER_ADDRESS="127.0.0.1:$port"
  export CORE_PEER_TLS_ROOTCERT_FILE="$base/peers/peer0.$org.roadpass.local/tls/ca.crt"
  export CORE_PEER_MSPCONFIGPATH="$base/users/$user@$org.roadpass.local/msp"
}

wait_http() { # wait_http <url> <name>
  for _ in $(seq 1 60); do
    if curl -fs "$1" >/dev/null 2>&1; then return 0; fi
    sleep 0.5
  done
  die "$2 did not become healthy — see $RUN/logs/$2.log"
}

start_bg() { # start_bg <name> <cmd...>   (env must already be exported)
  local name="$1"; shift
  mkdir -p "$RUN/logs" "$RUN/pids"
  nohup "$@" >"$RUN/logs/$name.log" 2>&1 &
  echo $! >"$RUN/pids/$name.pid"
}

is_running() { [ -f "$RUN/pids/$1.pid" ] && kill -0 "$(cat "$RUN/pids/$1.pid")" 2>/dev/null; }

start_orderer() {
  local o="$CRYPTO/ordererOrganizations/roadpass.local/orderers/orderer.roadpass.local"
  local lip="${FABRIC_LISTEN_IP:-127.0.0.1}"
  (
    export FABRIC_CFG_PATH="$RUN/config"
    export FABRIC_LOGGING_SPEC=INFO
    export ORDERER_GENERAL_LISTENADDRESS="$lip" ORDERER_GENERAL_LISTENPORT=7050
    export ORDERER_GENERAL_LOCALMSPID=OrdererMSP ORDERER_GENERAL_LOCALMSPDIR="$o/msp"
    export ORDERER_GENERAL_TLS_ENABLED=true
    export ORDERER_GENERAL_TLS_PRIVATEKEY="$o/tls/server.key" ORDERER_GENERAL_TLS_CERTIFICATE="$o/tls/server.crt"
    export ORDERER_GENERAL_TLS_ROOTCAS="$o/tls/ca.crt"
    export ORDERER_GENERAL_CLUSTER_CLIENTCERTIFICATE="$o/tls/server.crt" ORDERER_GENERAL_CLUSTER_CLIENTPRIVATEKEY="$o/tls/server.key"
    export ORDERER_GENERAL_CLUSTER_ROOTCAS="$o/tls/ca.crt"
    export ORDERER_GENERAL_BOOTSTRAPMETHOD=none ORDERER_CHANNELPARTICIPATION_ENABLED=true
    export ORDERER_FILELEDGER_LOCATION="$DATA/orderer"
    export ORDERER_CONSENSUS_WALDIR="$DATA/orderer/etcdraft/wal" ORDERER_CONSENSUS_SNAPDIR="$DATA/orderer/etcdraft/snap"
    export ORDERER_OPERATIONS_LISTENADDRESS="$lip:9443" ORDERER_ADMIN_LISTENADDRESS="$lip:7053"
    export ORDERER_METRICS_PROVIDER=prometheus   # /metrics on the operations endpoint
    start_bg orderer orderer
  )
  wait_http http://127.0.0.1:9443/healthz orderer
}

start_peer() { # start_peer <org> <port> <opsport>
  local org="$1" port="$2" ops="$3" msp
  local lip="${FABRIC_LISTEN_IP:-127.0.0.1}"
  case "$org" in sensifai) msp=SensifaiMSP;; plasmix) msp=PlasmixMSP;; esac
  local p="$CRYPTO/peerOrganizations/$org.roadpass.local/peers/peer0.$org.roadpass.local"
  (
    export FABRIC_CFG_PATH="$RUN/config"
    export FABRIC_LOGGING_SPEC="INFO:gossip,grpc=warn"
    export CORE_PEER_ID="peer0.$org.roadpass.local" CORE_PEER_LOCALMSPID="$msp" CORE_PEER_MSPCONFIGPATH="$p/msp"
    export CORE_PEER_TLS_ENABLED=true
    export CORE_PEER_TLS_CERT_FILE="$p/tls/server.crt" CORE_PEER_TLS_KEY_FILE="$p/tls/server.key" CORE_PEER_TLS_ROOTCERT_FILE="$p/tls/ca.crt"
    export CORE_PEER_LISTENADDRESS="$lip:$port" CORE_PEER_ADDRESS="127.0.0.1:$port"
    export CORE_PEER_CHAINCODELISTENADDRESS="$lip:$((port+1))" CORE_PEER_CHAINCODEADDRESS="127.0.0.1:$((port+1))"
    export CORE_PEER_GOSSIP_BOOTSTRAP="127.0.0.1:$port" CORE_PEER_GOSSIP_EXTERNALENDPOINT="127.0.0.1:$port"
    export CORE_PEER_FILESYSTEMPATH="$DATA/peer0.$org" CORE_LEDGER_SNAPSHOTS_ROOTDIR="$DATA/peer0.$org/snapshots"
    export CORE_OPERATIONS_LISTENADDRESS="$lip:$ops" CORE_METRICS_PROVIDER=prometheus
    start_bg "peer0.$org" peer node start
  )
  wait_http "http://127.0.0.1:$ops/healthz" "peer0.$org"
}

start_all() {
  start_orderer
  start_peer sensifai 7051 9444
  start_peer plasmix 9051 9445
}

# ------------------------------------------------------------------ up
cmd_up() {
  [ -x "$FAB/bin/peer" ] || cmd_install
  if is_running orderer; then log "network already running"; return; fi
  if [ -d "$DATA/orderer" ] && [ -d "$CRYPTO/ordererOrganizations" ]; then
    log "existing ledger data found — restarting processes (use 'clean' for a fresh network)"
    start_all; return
  fi
  mkdir -p "$RUN/config" "$ART"
  # generated peer/orderer config: default core.yaml with the ccaas builder pointed at our install
  # and Docker disabled (empty vm.endpoint): chaincode runs as a service, no containers
  sed -e "s|/opt/hyperledger/ccaas_builder|$FAB/builders/ccaas|" \
      -e "s|endpoint: unix:///var/run/docker.sock|endpoint: \"\"|" \
      "$FAB/config/core.yaml" >"$RUN/config/core.yaml"
  cp "$FAB/config/orderer.yaml" "$RUN/config/orderer.yaml"

  log "generating crypto material (cryptogen)"
  cryptogen generate --config="$HERE/crypto-config.yaml" --output="$CRYPTO" >/dev/null

  log "generating channel genesis blocks"
  (cd "$HERE" && FABRIC_CFG_PATH="$HERE" configtxgen -profile RoadpassChannel -outputBlock "$ART/roadpass.block" -channelID roadpass 2>/dev/null)
  (cd "$HERE" && FABRIC_CFG_PATH="$HERE" configtxgen -profile SandboxChannel  -outputBlock "$ART/sandbox.block"  -channelID sandbox 2>/dev/null)

  log "starting orderer + peers"
  start_all

  for ch in $CHANNELS; do
    osnadmin channel join --channelID "$ch" --config-block "$ART/$ch.block" -o 127.0.0.1:7053 >/dev/null
    log "orderer joined $ch"
  done
  sleep 2
  org_env sensifai; peer channel join -b "$ART/roadpass.block" 2>/dev/null; peer channel join -b "$ART/sandbox.block" 2>/dev/null
  org_env plasmix;  peer channel join -b "$ART/roadpass.block" 2>/dev/null
  log "peer0.sensifai joined roadpass, sandbox; peer0.plasmix joined roadpass"
  log "network is up"
}

# ------------------------------------------------------------------ deploy
cmd_deploy() {
  is_running orderer || die "network is not running — ./network.sh up"
  mkdir -p "$RUN/cc/pkg"
  if [ -x "$ROOT/chaincode/$CC_NAME/$CC_NAME" ]; then
    log "using pre-built $CC_NAME"
    cp "$ROOT/chaincode/$CC_NAME/$CC_NAME" "$RUN/cc/$CC_NAME"
  else
    command -v go >/dev/null || die "Go >= 1.24 is required to build the chaincode"
    log "building $CC_NAME"
    (cd "$ROOT/chaincode/$CC_NAME" && go build -o "$RUN/cc/$CC_NAME" .)
  fi

  # chaincode-as-a-service package: only connection info, the code runs as a local process.
  # Built once and kept: a re-built tarball gets a new package id (timestamps), which would no
  # longer match the installed + committed definition.
  local tgz="$RUN/cc/${CC_NAME}_${CC_VERSION}.tgz"
  if [ ! -f "$tgz" ]; then
    printf '{"address":"%s","dial_timeout":"10s","tls_required":false}\n' "$CC_ADDR" >"$RUN/cc/pkg/connection.json"
    printf '{"type":"ccaas","label":"%s_%s"}\n' "$CC_NAME" "$CC_VERSION" >"$RUN/cc/pkg/metadata.json"
    (cd "$RUN/cc/pkg" && tar -czf code.tar.gz connection.json && tar -czf "$tgz" metadata.json code.tar.gz)
  fi

  local pkgid installed
  org_env sensifai
  pkgid="$(peer lifecycle chaincode calculatepackageid "$tgz")"
  installed="$(peer lifecycle chaincode queryinstalled 2>/dev/null \
    | sed -n "s/^Package ID: \(${CC_NAME}_${CC_VERSION}:[0-9a-f]*\), Label.*/\1/p" | head -1)"
  if [ -n "$installed" ] && [ "$installed" != "$pkgid" ]; then
    log "package file differs from the installed package — using the installed id $installed"
    pkgid="$installed"
  fi
  for org in sensifai plasmix; do
    org_env "$org"
    if ! peer lifecycle chaincode queryinstalled 2>/dev/null | grep -q "$pkgid"; then
      [ "$(peer lifecycle chaincode calculatepackageid "$tgz")" = "$pkgid" ] \
        || die "peer0.$org lacks package $pkgid and $tgz is a different package — run: ./network.sh clean && ./network.sh up"
      peer lifecycle chaincode install "$tgz" >/dev/null 2>&1
      log "installed on peer0.$org"
    fi
  done
  echo "$pkgid" >"$RUN/cc/package_id"

  # (re)start the chaincode server so it runs the binary just built, under the installed package id
  if is_running chaincode; then kill "$(cat "$RUN/pids/chaincode.pid")"; sleep 1; fi
  ( export CHAINCODE_ID="$pkgid" CHAINCODE_SERVER_ADDRESS="$CC_ADDR" ROADPASS_OPERATOR_MSP=SensifaiMSP
    start_bg chaincode "$RUN/cc/$CC_NAME" )
  sleep 1; is_running chaincode || die "chaincode server failed — see $RUN/logs/chaincode.log"
  log "chaincode server running at $CC_ADDR ($pkgid)"

  approve_commit roadpass "OR('SensifaiMSP.peer','PlasmixMSP.peer')" "sensifai plasmix" "$pkgid"
  approve_commit sandbox  "OR('SensifaiMSP.peer')"                    "sensifai"         "$pkgid"
  log "deployed $CC_NAME v$CC_VERSION (seq $CC_SEQUENCE) on: $CHANNELS"
}

approve_commit() { # approve_commit <channel> <policy> <orgs> <pkgid>
  local ch="$1" pol="$2" orgs="$3" pkgid="$4" peers=""
  org_env sensifai
  if peer lifecycle chaincode querycommitted -C "$ch" -n "$CC_NAME" >/dev/null 2>&1; then
    log "$CC_NAME already committed on $ch"; return
  fi
  for org in $orgs; do
    org_env "$org"
    peer lifecycle chaincode approveformyorg -o 127.0.0.1:7050 --tls --cafile "$ORDERER_CA" \
      -C "$ch" -n "$CC_NAME" -v "$CC_VERSION" --sequence "$CC_SEQUENCE" --package-id "$pkgid" \
      --signature-policy "$pol" --waitForEvent >/dev/null 2>&1
    peers="$peers --peerAddresses $CORE_PEER_ADDRESS --tlsRootCertFiles $CORE_PEER_TLS_ROOTCERT_FILE"
  done
  org_env sensifai
  # shellcheck disable=SC2086
  peer lifecycle chaincode commit -o 127.0.0.1:7050 --tls --cafile "$ORDERER_CA" \
    -C "$ch" -n "$CC_NAME" -v "$CC_VERSION" --sequence "$CC_SEQUENCE" --signature-policy "$pol" $peers --waitForEvent >/dev/null 2>&1
  log "committed $CC_NAME on $ch ($pol)"
}

# ------------------------------------------------------------------ onboard
# Adds a value-chain actor org to channel roadpass, the way a new PLASMIX ROAD actor joins:
#   1. MSP + identities (cryptogen: Admin, client User1; NodeOUs) — Fabric CA in production
#   2. org definition (configtxgen -printOrg) inserted into the channel config (configtxlator compute_update)
#   3. update signed by the member-org admins (Application/Admins = MAJORITY) and submitted by the operator
#   4. done when User1 of the new org can read the channel through the operator peer
# The org is client-only (no peer): it transacts through the operator's gateway, endorsed by the member peers.
# On this single-host dev network every admin key is local; on a real network each admin signs separately.
cmd_onboard() {
  local org="$1"
  [[ "$org" =~ ^[a-z][a-z0-9]{1,30}$ ]] || die "usage: network.sh onboard <org>  (lower-case letters/digits, e.g. recycler)"
  [ "$org" != orderer ] || die "'orderer' is reserved"
  is_running orderer || die "network is not running — ./network.sh up"
  local msp dom w base ch=roadpass
  msp="$(printf '%s' "${org:0:1}" | tr '[:lower:]' '[:upper:]')${org:1}MSP"
  dom="$org.roadpass.local"
  base="$CRYPTO/peerOrganizations/$dom"
  w="$RUN/onboard/$org"
  rm -rf "$w"; mkdir -p "$w"

  if [ ! -d "$base" ]; then
    printf 'PeerOrgs:\n  - Name: %s\n    Domain: %s\n    EnableNodeOUs: true\n    Template: {Count: 0}\n    Users: {Count: 1}\n' \
      "${msp%MSP}" "$dom" >"$w/crypto-config.yaml"
    cryptogen generate --config="$w/crypto-config.yaml" --output="$CRYPTO" >/dev/null
  fi
  log "[$org] MSP $msp: Admin@$dom, User1@$dom"

  cat >"$w/configtx.yaml" <<EOF
Organizations:
  - Name: $msp
    ID: $msp
    MSPDir: $base/msp
    Policies:
      Readers:     {Type: Signature, Rule: "OR('$msp.admin', '$msp.client')"}
      Writers:     {Type: Signature, Rule: "OR('$msp.admin', '$msp.client')"}
      Admins:      {Type: Signature, Rule: "OR('$msp.admin')"}
      Endorsement: {Type: Signature, Rule: "OR('$msp.peer')"}
EOF
  FABRIC_CFG_PATH="$w" configtxgen -printOrg "$msp" >"$w/org.json" 2>/dev/null

  org_env sensifai
  peer channel fetch config "$w/config_block.pb" -o 127.0.0.1:7050 -c "$ch" --tls --cafile "$ORDERER_CA" 2>/dev/null \
    || die "[$org] cannot fetch the $ch config"
  configtxlator proto_decode --input "$w/config_block.pb" --type common.Block --output "$w/config_block.json"
  local members
  members="$(python3 - "$w" "$msp" <<'PY'
import json, sys
w, msp = sys.argv[1:]
cfg = json.load(open(f"{w}/config_block.json"))["data"]["data"][0]["payload"]["data"]["config"]
json.dump(cfg, open(f"{w}/config.json", "w"))
groups = cfg["channel_group"]["groups"]["Application"]["groups"]
if msp in groups:
    sys.exit(f"{msp} is already a member of the channel")
print(" ".join(sorted(groups)))
groups[msp] = json.load(open(f"{w}/org.json"))
json.dump(cfg, open(f"{w}/modified_config.json", "w"))
PY
)" || die "[$org] cannot build the config update"
  configtxlator proto_encode --input "$w/config.json" --type common.Config --output "$w/config.pb"
  configtxlator proto_encode --input "$w/modified_config.json" --type common.Config --output "$w/modified_config.pb"
  configtxlator compute_update --channel_id "$ch" --original "$w/config.pb" --updated "$w/modified_config.pb" --output "$w/update.pb"
  configtxlator proto_decode --input "$w/update.pb" --type common.ConfigUpdate --output "$w/update.json"
  python3 - "$w" "$ch" <<'PY'
import json, sys
w, ch = sys.argv[1:]
upd = json.load(open(f"{w}/update.json"))
env = {"payload": {"header": {"channel_header": {"channel_id": ch, "type": 2}}, "data": {"config_update": upd}}}
json.dump(env, open(f"{w}/update_envelope.json", "w"))
PY
  configtxlator proto_encode --input "$w/update_envelope.json" --type common.Envelope --output "$w/update_envelope.pb"
  log "[$org] config update computed (current members: $members)"

  local m mdom signers=""
  for m in $members; do
    [ "$m" = SensifaiMSP ] && continue   # the operator signs when it submits
    mdom="$(printf '%s' "${m%MSP}" | tr '[:upper:]' '[:lower:]').roadpass.local"
    CORE_PEER_LOCALMSPID="$m" CORE_PEER_MSPCONFIGPATH="$CRYPTO/peerOrganizations/$mdom/users/Admin@$mdom/msp" \
      peer channel signconfigtx -f "$w/update_envelope.pb" 2>/dev/null
    signers="$signers $m"
  done
  org_env sensifai
  peer channel update -f "$w/update_envelope.pb" -c "$ch" -o 127.0.0.1:7050 --tls --cafile "$ORDERER_CA" 2>/dev/null \
    || die "[$org] channel update rejected"
  log "[$org] channel $ch updated (signed by$signers SensifaiMSP)"

  # usable = the new client identity passes the channel Readers policy on the operator peer
  local i
  for i in $(seq 1 40); do
    if CORE_PEER_LOCALMSPID="$msp" CORE_PEER_MSPCONFIGPATH="$base/users/User1@$dom/msp" \
       peer chaincode query -C "$ch" -n qscc -c "{\"Args\":[\"GetChainInfo\",\"$ch\"]}" >/dev/null 2>&1; then
      log "[$org] onboarded — User1@$dom reads $ch via peer0.sensifai"
      return 0
    fi
    sleep 0.5
  done
  die "[$org] User1@$dom still cannot read $ch"
}

# ------------------------------------------------------------------ status/down/clean
cmd_status() {
  for p in orderer peer0.sensifai peer0.plasmix chaincode; do
    if is_running "$p"; then echo "  ✔ $p (pid $(cat "$RUN/pids/$p.pid"))"; else echo "  ✘ $p"; fi
  done
  if is_running chaincode; then
    org_env sensifai user
    echo "  anchored on roadpass: $(peer chaincode query -C roadpass -n "$CC_NAME" -c '{"Args":["Stats"]}' 2>/dev/null)"
  fi
}

cmd_down() {
  for p in chaincode peer0.plasmix peer0.sensifai orderer; do
    if is_running "$p"; then kill "$(cat "$RUN/pids/$p.pid")" && log "stopped $p"; fi
    rm -f "$RUN/pids/$p.pid"
  done
  sleep 1
}

cmd_restart() {
  cmd_down
  start_all
  if [ -f "$RUN/cc/package_id" ]; then
    ( export CHAINCODE_ID="$(cat "$RUN/cc/package_id")" CHAINCODE_SERVER_ADDRESS="$CC_ADDR" ROADPASS_OPERATOR_MSP=SensifaiMSP
      start_bg chaincode "$RUN/cc/$CC_NAME" )
  fi
  log "restarted on existing ledger data"
}

cmd_backup() {
  [ -d "$DATA" ] || die "nothing to back up"
  mkdir -p "$HERE/backups"
  local f="$HERE/backups/roadpass-ledger-$(date -u +%Y%m%dT%H%M%SZ).tgz"
  cmd_down
  (cd "$HERE" && tar -czf "$f" data crypto artifacts .run/config .run/cc)
  log "backup written: $f ($(du -h "$f" | cut -f1))"
  cmd_restart
  echo "$f" >"$HERE/backups/LATEST"
}

cmd_restore() {
  local f="${1:-$(cat "$HERE/backups/LATEST" 2>/dev/null || true)}"
  [ -f "$f" ] || die "backup archive not found: $f"
  cmd_down
  rm -rf "$DATA" "$CRYPTO" "$ART" "$RUN"
  log "wiped ledger data, crypto and config"
  (cd "$HERE" && tar -xzf "$f")
  log "restored from $f"
  cmd_restart
}

cmd_clean() {
  cmd_down
  rm -rf "$DATA" "$CRYPTO" "$ART" "$RUN"
  log "removed ledger data, crypto and generated config (binaries kept in $FAB)"
}

case "${1:-}" in
  install) cmd_install;;
  up)      cmd_up;;
  deploy)  cmd_deploy;;
  status)  cmd_status;;
  down)    cmd_down;;
  restart) cmd_restart;;
  backup)  cmd_backup;;
  restore) cmd_restore "${2:-}";;
  onboard) cmd_onboard "${2:-}";;
  clean)   cmd_clean;;
  *) sed -n '2,16p' "$0"; exit 1;;
esac
