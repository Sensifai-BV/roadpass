#!/usr/bin/env bash
# Starts (or restarts) the network, deploys the chaincode, then follows the logs.
set -euo pipefail
cd /roadpass/fabric
rm -f .run/pids/*.pid /tmp/fabric-ready   # pids of a previous container are meaningless here
./network.sh up
./network.sh deploy
# The service container reads the client identity and TLS CA from the shared
# crypto volume as a non-root user. This is a local development network.
chmod -R a+rX crypto
touch /tmp/fabric-ready
echo "[fabric] ready: channel roadpass, chaincode roadpass-proof"
trap './network.sh down; exit 0' TERM INT
tail -n 0 -F .run/logs/*.log &
wait $!
