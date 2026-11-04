#!/bin/sh
# Writes the Hyperledger Explorer connection profile from the shared crypto volume.
# Explorer is a debugging aid for operators (compose profile "debug"), not part of
# the product; the dashboard has its own ledger explorer.
set -eu
C=/crypto/peerOrganizations/sensifai.roadpass.local
key=$(ls "$C"/users/Admin@sensifai.roadpass.local/msp/keystore/* | head -1)
cert=$(ls "$C"/users/Admin@sensifai.roadpass.local/msp/signcerts/* | head -1)
mkdir -p /state/connection-profile
cat > /state/config.json <<JSON
{"network-configs":{"roadpass":{"name":"ROADPASS (local)","profile":"./connection-profile/roadpass.json"}},"license":"Apache-2.0"}
JSON
cat > /state/connection-profile/roadpass.json <<JSON
{
  "name": "roadpass", "version": "1.0.0", "license": "Apache-2.0",
  "client": {"tlsEnable": true, "enableAuthentication": false, "organization": "SensifaiMSP",
    "adminCredential": {"id": "exploreradmin", "password": "${EXPLORER_ADMIN_PASSWORD:-roadpass-debug}"},
    "connection": {"timeout": {"peer": {"endorser": "300"}, "orderer": "300"}}},
  "channels": {"roadpass": {"peers": {"peer0.sensifai.roadpass.local": {}}}, "sandbox": {"peers": {"peer0.sensifai.roadpass.local": {}}}},
  "organizations": {"SensifaiMSP": {"mspid": "SensifaiMSP", "peers": ["peer0.sensifai.roadpass.local"],
    "adminPrivateKey": {"path": "$key"}, "signedCert": {"path": "$cert"}}},
  "peers": {"peer0.sensifai.roadpass.local": {"url": "grpcs://127.0.0.1:7051",
    "tlsCACerts": {"path": "$C/peers/peer0.sensifai.roadpass.local/tls/ca.crt"},
    "grpcOptions": {"ssl-target-name-override": "peer0.sensifai.roadpass.local"}}}
}
JSON
echo "explorer config written"
