// roadpass-proof — ROADPASS proof-layer chaincode (Hyperledger Fabric 2.5).
//
// Anchors SHA-256 hashes of Digital Material Passport records, import batches
// (Merkle roots) and per-LDPE-batch evidence packages. No business data is
// stored on-chain: only hashes, identifiers, provenance labels and assurance
// status.
//
// Runs as chaincode-as-a-service when CHAINCODE_SERVER_ADDRESS is set,
// otherwise as a classic peer-launched chaincode.
package main

import (
	"log"
	"os"

	"github.com/hyperledger/fabric-chaincode-go/v2/shim"
	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
)

func main() {
	cc, err := contractapi.NewChaincode(&ProofContract{})
	if err != nil {
		log.Fatalf("create chaincode: %v", err)
	}
	cc.Info.Title = "roadpass-proof"
	cc.Info.Version = "1.0.0"

	addr := os.Getenv("CHAINCODE_SERVER_ADDRESS")
	if addr == "" {
		if err := cc.Start(); err != nil {
			log.Fatalf("start chaincode: %v", err)
		}
		return
	}
	server := &shim.ChaincodeServer{
		CCID:     os.Getenv("CHAINCODE_ID"),
		Address:  addr,
		CC:       cc,
		TLSProps: shim.TLSProperties{Disabled: true},
	}
	log.Printf("roadpass-proof listening on %s (CCID %s)", addr, server.CCID)
	if err := server.Start(); err != nil {
		log.Fatalf("chaincode server: %v", err)
	}
}
