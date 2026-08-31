# A self-contained Hyperledger Fabric 2.5 LTS network for local E2E and demos:
# one Raft orderer, peer0.sensifai and peer0.plasmix, and the roadpass-proof
# chaincode running as a service, all as native processes in one container
# (fabric/network.sh). Production uses the SensiTrace network instead.
FROM golang:1.27-bookworm AS cc
WORKDIR /src
COPY chaincode/roadpass-proof ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/roadpass-proof .

FROM debian:bookworm-slim
ARG FABRIC_VERSION=2.5.16
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends bash ca-certificates curl procps tar \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /opt/fabric \
 && curl -fsSL "https://github.com/hyperledger/fabric/releases/download/v${FABRIC_VERSION}/hyperledger-fabric-linux-${TARGETARCH:-amd64}-${FABRIC_VERSION}.tar.gz" | tar -xz -C /opt/fabric
WORKDIR /roadpass
COPY fabric/network.sh fabric/configtx.yaml fabric/crypto-config.yaml fabric/
COPY --from=cc /out/roadpass-proof chaincode/roadpass-proof/roadpass-proof
COPY deploy/fabric/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh fabric/network.sh
ENV FABRIC_HOME=/opt/fabric FABRIC_LISTEN_IP=0.0.0.0 PATH=/opt/fabric/bin:$PATH
EXPOSE 7050 7051 9051
HEALTHCHECK --interval=5s --timeout=3s --retries=60 --start-period=10s CMD test -f /tmp/fabric-ready && curl -fs http://127.0.0.1:9444/healthz >/dev/null
ENTRYPOINT ["/entrypoint.sh"]
