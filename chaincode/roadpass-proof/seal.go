package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
)

// Seals and signers (contract v2).
//
// A seal anchors only the Merkle root of up to MaxSealLeaves record hashes. The
// leaves travel in the transaction so the chaincode can recompute the root (the
// ledger never trusts the client's root) and so any channel member can rebuild an
// inclusion proof from the block, but they are not written to world state: 3M
// records cost ~730 state keys instead of 3M.
//
// A signer is an organisation's Ed25519 public key. Registering it on the ledger
// makes record signatures verifiable without trusting the platform's database.

const (
	keySeal   = "seal"   // seal~<sealId>
	keySigner = "signer" // signer~<keyId>

	// MaxSealLeaves bounds the transaction size (4096 × 64 hex chars ≈ 260 KB).
	MaxSealLeaves = 4096
)

var (
	sealIDRE        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	sealProvenance  = []string{"Operational", "Simulated", "PublicReference", "Mixed", "AnonymisedSynthetic"}
	signerAlgorithm = "Ed25519"
)

// SealInput is what the sealer submits.
type SealInput struct {
	SealID     string   `json:"sealId"`
	MerkleRoot string   `json:"merkleRoot"`
	Leaves     []string `json:"leaves"`
	FirstSeq   int64    `json:"firstSeq"`
	LastSeq    int64    `json:"lastSeq"`
	Provenance string   `json:"provenance"`
}

// Seal is the on-chain record of one sealed set of record hashes.
type Seal struct {
	DocType      string `json:"docType"`
	SealID       string `json:"sealId"`
	MerkleRoot   string `json:"merkleRoot"`
	LeafCount    int    `json:"leafCount"`
	FirstSeq     int64  `json:"firstSeq"`
	LastSeq      int64  `json:"lastSeq"`
	Provenance   string `json:"provenance"`
	SubmitterMSP string `json:"submitterMsp"`
	TxID         string `json:"txId"`
	SealedAt     string `json:"sealedAt"`
}

// SignerInput registers one organisation signing key.
type SignerInput struct {
	KeyID     string `json:"keyId"`
	OrgID     string `json:"orgId"`
	OrgName   string `json:"orgName"`
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"publicKey"` // base64 (std) of the 32-byte Ed25519 key
	// HolderID names the person a personal key belongs to: a pseudonymous user id
	// only, never a name or e-mail (the ledger cannot forget). Empty for company keys.
	HolderID string `json:"holderId,omitempty"`
}

// Signer is the on-chain trust anchor for one signing key.
type Signer struct {
	DocType      string `json:"docType"`
	KeyID        string `json:"keyId"`
	OrgID        string `json:"orgId"`
	OrgName      string `json:"orgName"`
	HolderID     string `json:"holderId,omitempty" metadata:",optional"`
	Algorithm    string `json:"algorithm"`
	PublicKey    string `json:"publicKey"`
	Status       string `json:"status"` // active | revoked
	RevokeReason string `json:"revokeReason,omitempty" metadata:",optional"`
	RegisteredAt string `json:"registeredAt"`
	RevokedAt    string `json:"revokedAt,omitempty" metadata:",optional"`
	TxID         string `json:"txId"`
}

// SealBatch anchors the Merkle root over a set of record hashes. Operator only.
func (c *ProofContract) SealBatch(ctx contractapi.TransactionContextInterface, sealJSON string) (*Seal, error) {
	who, err := submitter(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOperator(who); err != nil {
		return nil, err
	}
	var in SealInput
	if err := json.Unmarshal([]byte(sealJSON), &in); err != nil {
		return nil, fmt.Errorf("invalid seal JSON: %w", err)
	}
	switch {
	case !sealIDRE.MatchString(in.SealID):
		return nil, fmt.Errorf("sealId %q is not a valid identifier", in.SealID)
	case !hashRE.MatchString(in.MerkleRoot):
		return nil, fmt.Errorf("merkleRoot must be 64 lowercase hex chars")
	case len(in.Leaves) == 0:
		return nil, fmt.Errorf("seal has no leaves")
	case len(in.Leaves) > MaxSealLeaves:
		return nil, fmt.Errorf("seal has %d leaves, the maximum is %d", len(in.Leaves), MaxSealLeaves)
	case in.FirstSeq < 0 || in.LastSeq < in.FirstSeq:
		return nil, fmt.Errorf("invalid sequence range %d..%d", in.FirstSeq, in.LastSeq)
	case !slices.Contains(sealProvenance, in.Provenance):
		return nil, fmt.Errorf("provenance %q not in %v", in.Provenance, sealProvenance)
	}
	key, err := ctx.GetStub().CreateCompositeKey(keySeal, []string{in.SealID})
	if err != nil {
		return nil, err
	}
	if existing, err := ctx.GetStub().GetState(key); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, fmt.Errorf("seal %s already exists", in.SealID)
	}
	leaves := make([][]byte, len(in.Leaves))
	for i, l := range in.Leaves {
		if !hashRE.MatchString(l) {
			return nil, fmt.Errorf("leaf %d must be 64 lowercase hex chars", i)
		}
		leaves[i], _ = hex.DecodeString(l)
	}
	if got := hex.EncodeToString(MerkleRoot(leaves)); got != in.MerkleRoot {
		return nil, fmt.Errorf("merkle root mismatch: client %s, recomputed %s", in.MerkleRoot, got)
	}
	ts, err := txTime(ctx)
	if err != nil {
		return nil, err
	}
	s := &Seal{
		DocType: "seal", SealID: in.SealID, MerkleRoot: in.MerkleRoot, LeafCount: len(in.Leaves),
		FirstSeq: in.FirstSeq, LastSeq: in.LastSeq, Provenance: in.Provenance,
		SubmitterMSP: who.msp, TxID: ctx.GetStub().GetTxID(), SealedAt: ts,
	}
	return s, putJSON(ctx, key, s)
}

// GetSeal returns one seal.
func (c *ProofContract) GetSeal(ctx contractapi.TransactionContextInterface, sealID string) (*Seal, error) {
	k, err := ctx.GetStub().CreateCompositeKey(keySeal, []string{sealID})
	if err != nil {
		return nil, err
	}
	var s Seal
	if ok, err := getJSON(ctx, k, &s); err != nil || !ok {
		return nil, notFound("seal", sealID, err)
	}
	return &s, nil
}

// RegisterSigner puts an organisation's public signing key on the ledger. Operator
// only: the operator vouches that the key belongs to the organisation. A key id is
// registered once; rotation is a new key id plus RevokeSigner on the old one.
func (c *ProofContract) RegisterSigner(ctx contractapi.TransactionContextInterface, signerJSON string) (*Signer, error) {
	who, err := submitter(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOperator(who); err != nil {
		return nil, err
	}
	var in SignerInput
	if err := json.Unmarshal([]byte(signerJSON), &in); err != nil {
		return nil, fmt.Errorf("invalid signer JSON: %w", err)
	}
	pk, err := base64.StdEncoding.DecodeString(in.PublicKey)
	switch {
	case !sealIDRE.MatchString(in.KeyID):
		return nil, fmt.Errorf("keyId %q is not a valid identifier", in.KeyID)
	case strings.TrimSpace(in.OrgID) == "":
		return nil, fmt.Errorf("orgId is required")
	case in.Algorithm != signerAlgorithm:
		return nil, fmt.Errorf("algorithm %q not supported (want %s)", in.Algorithm, signerAlgorithm)
	case err != nil || len(pk) != 32:
		return nil, fmt.Errorf("publicKey must be a base64 32-byte Ed25519 key")
	case in.HolderID != "" && !sealIDRE.MatchString(in.HolderID):
		return nil, fmt.Errorf("holderId %q is not a valid identifier", in.HolderID)
	}
	key, err := ctx.GetStub().CreateCompositeKey(keySigner, []string{in.KeyID})
	if err != nil {
		return nil, err
	}
	if existing, err := ctx.GetStub().GetState(key); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, fmt.Errorf("signer %s already registered", in.KeyID)
	}
	ts, err := txTime(ctx)
	if err != nil {
		return nil, err
