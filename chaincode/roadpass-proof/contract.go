package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
)

// Object-type prefixes for composite keys.
const (
	keyRecord  = "rec"    // rec~<recordId>~<version:06d>
	keyLatest  = "latest" // latest~<recordId> -> version
	keyBatch   = "batch"  // batch~<importBatchId>
	keyPackage = "pkg"    // pkg~<packageId>
)

var (
	hashRE             = regexp.MustCompile(`^[0-9a-f]{64}$`)
	allowedProvenance  = []string{"Operational", "AnonymisedSynthetic", "PublicReference", "Mixed"}
	approvedAssurance  = []string{"pass", "accepted_override"}
	defaultOperatorMSP = "SensifaiMSP"
)

// ProofContract implements the ROADPASS proof layer.
type ProofContract struct {
	contractapi.Contract
}

// RecordInput is what a client submits for one passport record.
type RecordInput struct {
	RecordID        string `json:"recordId"`
	Version         int    `json:"version"`
	EntityType      string `json:"entityType"`
	ContentHash     string `json:"contentHash"`
	PrevHash        string `json:"prevHash"`
	Provenance      string `json:"provenance"`
	AssuranceStatus string `json:"assuranceStatus"`
}

// RecordAnchor is the on-chain proof of one record version.
type RecordAnchor struct {
	DocType         string `json:"docType"`
	RecordID        string `json:"recordId"`
	Version         int    `json:"version"`
	EntityType      string `json:"entityType"`
	ContentHash     string `json:"contentHash"`
	PrevHash        string `json:"prevHash"`
	Provenance      string `json:"provenance"`
	AssuranceStatus string `json:"assuranceStatus"`
	ImportBatchID   string `json:"importBatchId"`
	MerkleIndex     int    `json:"merkleIndex"`
	SubmitterMSP    string `json:"submitterMsp"`
	SubmitterID     string `json:"submitterId"`
	TxID            string `json:"txId"`
	AnchoredAt      string `json:"anchoredAt"`
}

// RecordRef points at one anchored record version.
type RecordRef struct {
	RecordID string `json:"recordId"`
	Version  int    `json:"version"`
}

// ImportBatch is the on-chain proof of one import (Merkle root over records).
type ImportBatch struct {
	DocType       string `json:"docType"`
	ImportBatchID string `json:"importBatchId"`
	MerkleRoot    string `json:"merkleRoot"`
	RecordCount   int    `json:"recordCount"`
	SubmitterMSP  string `json:"submitterMsp"`
	TxID          string `json:"txId"`
	AnchoredAt    string `json:"anchoredAt"`
}

// EvidencePackageInput is what a client submits for one LDPE batch package.
type EvidencePackageInput struct {
	PackageID           string      `json:"packageId"`
	LDPEBatchID         string      `json:"ldpeBatchId"`
	PackageHash         string      `json:"packageHash"`
	AssuranceResultHash string      `json:"assuranceResultHash"`
	Members             []RecordRef `json:"members"`
}

// EvidencePackage is the on-chain proof of a recycled-content evidence package.
type EvidencePackage struct {
	DocType             string      `json:"docType"`
	PackageID           string      `json:"packageId"`
	LDPEBatchID         string      `json:"ldpeBatchId"`
	PackageHash         string      `json:"packageHash"`
	AssuranceResultHash string      `json:"assuranceResultHash"`
	Members             []RecordRef `json:"members"`
	SubmitterMSP        string      `json:"submitterMsp"`
	TxID                string      `json:"txId"`
	AnchoredAt          string      `json:"anchoredAt"`
}

// VerifyResult tells a caller whether an off-chain record matches its anchor.
type VerifyResult struct {
	RecordID    string        `json:"recordId"`
	Version     int           `json:"version"`
	Status      string        `json:"status"` // verified | mismatch | not_anchored
	Match       bool          `json:"match"`
	Anchor      *RecordAnchor `json:"anchor,omitempty" metadata:",optional"`
	GivenHash   string        `json:"givenHash"`
	LatestVer   int           `json:"latestVersion"`
	IsLatestVer bool          `json:"isLatestVersion"`
}

// Stats summarises what is anchored (used for KPI reporting).
type Stats struct {
	RecordVersions   int `json:"recordVersions"`
	DistinctRecords  int `json:"distinctRecords"`
	ImportBatches    int `json:"importBatches"`
	EvidencePackages int `json:"evidencePackages"`
}

// ---------------------------------------------------------------- writes

// AnchorRecord anchors a single record version (e.g. a correction -> version n+1).
// Any client identity of a channel member org may call it.
func (c *ProofContract) AnchorRecord(ctx contractapi.TransactionContextInterface, recordJSON string) (*RecordAnchor, error) {
	who, err := submitter(ctx)
	if err != nil {
		return nil, err
	}
	var in RecordInput
	if err := json.Unmarshal([]byte(recordJSON), &in); err != nil {
		return nil, fmt.Errorf("invalid record JSON: %w", err)
	}
	ts, err := txTime(ctx)
	if err != nil {
		return nil, err
	}
	a, err := c.anchor(ctx, in, map[string]pending{}, who, "", -1, ts)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// AnchorImportBatch anchors every record of one import plus the Merkle root
// over their content hashes, atomically, in one transaction. Operator only.
func (c *ProofContract) AnchorImportBatch(ctx contractapi.TransactionContextInterface, importBatchID string, merkleRoot string, recordsJSON string) (*ImportBatch, error) {
	who, err := submitter(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOperator(who); err != nil {
		return nil, err
	}
	if strings.TrimSpace(importBatchID) == "" {
		return nil, fmt.Errorf("importBatchId is required")
	}
	if !hashRE.MatchString(merkleRoot) {
		return nil, fmt.Errorf("merkleRoot must be 64 lowercase hex chars")
	}
	bk, err := ctx.GetStub().CreateCompositeKey(keyBatch, []string{importBatchID})
	if err != nil {
		return nil, err
	}
	if existing, err := ctx.GetStub().GetState(bk); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, fmt.Errorf("import batch %s already anchored", importBatchID)
	}

	var ins []RecordInput
	if err := json.Unmarshal([]byte(recordsJSON), &ins); err != nil {
		return nil, fmt.Errorf("invalid records JSON: %w", err)
	}
	if len(ins) == 0 {
		return nil, fmt.Errorf("import batch has no records")
	}

	// Recompute the Merkle root on-chain: the ledger never trusts the client's root.
	leaves := make([][]byte, len(ins))
	for i, r := range ins {
		b, err := hex.DecodeString(r.ContentHash)
		if err != nil || len(b) != 32 {
			return nil, fmt.Errorf("record %d (%s): contentHash must be 64 hex chars", i, r.RecordID)
		}
		leaves[i] = b
	}
	if got := hex.EncodeToString(MerkleRoot(leaves)); got != merkleRoot {
		return nil, fmt.Errorf("merkle root mismatch: client %s, recomputed %s", merkleRoot, got)
	}

	ts, err := txTime(ctx)
	if err != nil {
		return nil, err
	}
	inTx := map[string]pending{} // Fabric does not read its own writes: track them here
	for i, r := range ins {
		if _, err := c.anchor(ctx, r, inTx, who, importBatchID, i, ts); err != nil {
			return nil, fmt.Errorf("record %d: %w", i, err)
		}
	}

	b := &ImportBatch{
		DocType: "importBatch", ImportBatchID: importBatchID, MerkleRoot: merkleRoot,
		RecordCount: len(ins), SubmitterMSP: who.msp, TxID: ctx.GetStub().GetTxID(), AnchoredAt: ts,
	}
	return b, putJSON(ctx, bk, b)
}

// AnchorEvidencePackage anchors the hash of one recycled-content evidence
// package. Every member record must already be anchored with an approved
// assurance status. Operator only.
func (c *ProofContract) AnchorEvidencePackage(ctx contractapi.TransactionContextInterface, packageJSON string) (*EvidencePackage, error) {
	who, err := submitter(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOperator(who); err != nil {
		return nil, err
	}
	var in EvidencePackageInput
	if err := json.Unmarshal([]byte(packageJSON), &in); err != nil {
		return nil, fmt.Errorf("invalid package JSON: %w", err)
	}
	switch {
	case in.PackageID == "":
		return nil, fmt.Errorf("packageId is required")
	case in.LDPEBatchID == "":
		return nil, fmt.Errorf("ldpeBatchId is required")
	case !hashRE.MatchString(in.PackageHash):
		return nil, fmt.Errorf("packageHash must be 64 lowercase hex chars")
	case !hashRE.MatchString(in.AssuranceResultHash):
		return nil, fmt.Errorf("assuranceResultHash must be 64 lowercase hex chars")
	case len(in.Members) == 0:
		return nil, fmt.Errorf("package has no member records")
	}
	pk, err := ctx.GetStub().CreateCompositeKey(keyPackage, []string{in.PackageID})
	if err != nil {
		return nil, err
	}
	if existing, err := ctx.GetStub().GetState(pk); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, fmt.Errorf("evidence package %s already anchored", in.PackageID)
	}
	hasBatch := false
	for _, m := range in.Members {
		a, err := getAnchor(ctx, m.RecordID, m.Version)
		if err != nil {
			return nil, err
		}
		if a == nil {
			return nil, fmt.Errorf("member %s v%d is not anchored", m.RecordID, m.Version)
		}
		if !slices.Contains(approvedAssurance, a.AssuranceStatus) {
			return nil, fmt.Errorf("member %s v%d has assurance status %q", m.RecordID, m.Version, a.AssuranceStatus)
		}
		if m.RecordID == in.LDPEBatchID {
			hasBatch = true
		}
	}
	if !hasBatch {
		return nil, fmt.Errorf("package must include the LDPE batch record %s", in.LDPEBatchID)
	}
	ts, err := txTime(ctx)
	if err != nil {
		return nil, err
	}
	p := &EvidencePackage{
		DocType: "evidencePackage", PackageID: in.PackageID, LDPEBatchID: in.LDPEBatchID,
		PackageHash: in.PackageHash, AssuranceResultHash: in.AssuranceResultHash, Members: in.Members,
		SubmitterMSP: who.msp, TxID: ctx.GetStub().GetTxID(), AnchoredAt: ts,
	}
	return p, putJSON(ctx, pk, p)
}

// ---------------------------------------------------------------- reads

// Verify compares a hash computed off-chain with the anchored hash.
// version <= 0 means "latest anchored version".
func (c *ProofContract) Verify(ctx contractapi.TransactionContextInterface, recordID string, version int, contentHash string) (*VerifyResult, error) {
	latest, err := latestVersion(ctx, recordID)
	if err != nil {
		return nil, err
	}
	if version <= 0 {
		version = latest
	}
	res := &VerifyResult{RecordID: recordID, Version: version, GivenHash: contentHash, LatestVer: latest, Status: "not_anchored"}
	if version == 0 {
		return res, nil
	}
	a, err := getAnchor(ctx, recordID, version)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return res, nil
	}
	res.Anchor = a
	res.IsLatestVer = version == latest
	res.Match = a.ContentHash == strings.ToLower(contentHash)
	if res.Match {
		res.Status = "verified"
	} else {
		res.Status = "mismatch"
	}
	return res, nil
}

// History returns every anchored version of a record, oldest first.
func (c *ProofContract) History(ctx contractapi.TransactionContextInterface, recordID string) ([]*RecordAnchor, error) {
	it, err := ctx.GetStub().GetStateByPartialCompositeKey(keyRecord, []string{recordID})
	if err != nil {
		return nil, err
	}
	defer it.Close()
	out := []*RecordAnchor{}
	for it.HasNext() {
		kv, err := it.Next()
		if err != nil {
			return nil, err
		}
		var a RecordAnchor
		if err := json.Unmarshal(kv.Value, &a); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, nil
}

// GetImportBatch returns one anchored import batch.
func (c *ProofContract) GetImportBatch(ctx contractapi.TransactionContextInterface, importBatchID string) (*ImportBatch, error) {
	k, err := ctx.GetStub().CreateCompositeKey(keyBatch, []string{importBatchID})
	if err != nil {
		return nil, err
	}
	var b ImportBatch
	if ok, err := getJSON(ctx, k, &b); err != nil || !ok {
		return nil, notFound("import batch", importBatchID, err)
	}
	return &b, nil
}

// GetEvidencePackage returns one anchored evidence package.
func (c *ProofContract) GetEvidencePackage(ctx contractapi.TransactionContextInterface, packageID string) (*EvidencePackage, error) {
	k, err := ctx.GetStub().CreateCompositeKey(keyPackage, []string{packageID})
	if err != nil {
		return nil, err
	}
	var p EvidencePackage
	if ok, err := getJSON(ctx, k, &p); err != nil || !ok {
		return nil, notFound("evidence package", packageID, err)
	}
	return &p, nil
}

// Stats counts anchored objects.
func (c *ProofContract) Stats(ctx contractapi.TransactionContextInterface) (*Stats, error) {
	s := &Stats{}
	var err error
	if s.RecordVersions, err = count(ctx, keyRecord); err != nil {
		return nil, err
	}
	if s.DistinctRecords, err = count(ctx, keyLatest); err != nil {
		return nil, err
	}
	if s.ImportBatches, err = count(ctx, keyBatch); err != nil {
		return nil, err
	}
	if s.EvidencePackages, err = count(ctx, keyPackage); err != nil {
		return nil, err
	}
	return s, nil
}

// ---------------------------------------------------------------- internals

type identity struct{ msp, id string }

type pending struct {
	version int
	hash    string
}

func (c *ProofContract) anchor(ctx contractapi.TransactionContextInterface, in RecordInput, inTx map[string]pending, who identity, batchID string, idx int, ts string) (*RecordAnchor, error) {
	if err := validate(in); err != nil {
		return nil, err
	}
	latest, latestHash := 0, ""
	if p, ok := inTx[in.RecordID]; ok {
		latest, latestHash = p.version, p.hash
	} else {
		v, err := latestVersion(ctx, in.RecordID)
		if err != nil {
			return nil, err
		}
		if v > 0 {
			a, err := getAnchor(ctx, in.RecordID, v)
			if err != nil {
				return nil, err
			}
			latest, latestHash = v, a.ContentHash
		}
	}
	if in.Version != latest+1 {
		return nil, fmt.Errorf("%s: version %d rejected, next version must be %d (anchors are immutable)", in.RecordID, in.Version, latest+1)
	}
	if in.PrevHash != latestHash {
		return nil, fmt.Errorf("%s: prevHash %q does not match anchored v%d hash %q", in.RecordID, in.PrevHash, latest, latestHash)
	}
	a := &RecordAnchor{
		DocType: "record", RecordID: in.RecordID, Version: in.Version, EntityType: in.EntityType,
		ContentHash: in.ContentHash, PrevHash: in.PrevHash, Provenance: in.Provenance,
		AssuranceStatus: in.AssuranceStatus, ImportBatchID: batchID, MerkleIndex: idx,
		SubmitterMSP: who.msp, SubmitterID: who.id, TxID: ctx.GetStub().GetTxID(), AnchoredAt: ts,
	}
	rk, err := ctx.GetStub().CreateCompositeKey(keyRecord, []string{in.RecordID, fmt.Sprintf("%06d", in.Version)})
	if err != nil {
		return nil, err
	}
	if err := putJSON(ctx, rk, a); err != nil {
		return nil, err
	}
	lk, err := ctx.GetStub().CreateCompositeKey(keyLatest, []string{in.RecordID})
	if err != nil {
		return nil, err
	}
	if err := ctx.GetStub().PutState(lk, []byte(strconv.Itoa(in.Version))); err != nil {
		return nil, err
	}
	inTx[in.RecordID] = pending{in.Version, in.ContentHash}
	return a, nil
}

func validate(in RecordInput) error {
	switch {
	case !strings.HasPrefix(in.RecordID, "urn:ngsi-ld:"):
		return fmt.Errorf("recordId %q must be an NGSI-LD URN", in.RecordID)
	case in.EntityType == "":
		return fmt.Errorf("%s: entityType is required", in.RecordID)
	case !hashRE.MatchString(in.ContentHash):
		return fmt.Errorf("%s: contentHash must be 64 lowercase hex chars", in.RecordID)
	case in.PrevHash != "" && !hashRE.MatchString(in.PrevHash):
		return fmt.Errorf("%s: prevHash must be empty or 64 lowercase hex chars", in.RecordID)
	case !slices.Contains(allowedProvenance, in.Provenance):
		return fmt.Errorf("%s: provenance %q not in %v", in.RecordID, in.Provenance, allowedProvenance)
	case !slices.Contains(approvedAssurance, in.AssuranceStatus):
		return fmt.Errorf("%s: only approved records may be anchored (assuranceStatus %q)", in.RecordID, in.AssuranceStatus)
	}
	return nil
}

func submitter(ctx contractapi.TransactionContextInterface) (identity, error) {
	cid := ctx.GetClientIdentity()
	msp, err := cid.GetMSPID()
	if err != nil {
		return identity{}, err
	}
	cert, err := cid.GetX509Certificate()
	if err != nil || cert == nil {
		return identity{}, fmt.Errorf("cannot read submitter certificate: %v", err)
	}
	ous := cert.Subject.OrganizationalUnit
	if !slices.Contains(ous, "client") && !slices.Contains(ous, "admin") {
		return identity{}, fmt.Errorf("access denied: submitter must be a client or admin identity (OU=%v)", ous)
	}
	return identity{msp: msp, id: cert.Subject.CommonName}, nil
}

func requireOperator(who identity) error {
	op := os.Getenv("ROADPASS_OPERATOR_MSP")
	if op == "" {
		op = defaultOperatorMSP
	}
	if who.msp != op {
		return fmt.Errorf("access denied: %s may not perform operator actions (operator MSP is %s)", who.msp, op)
	}
	return nil
}

func latestVersion(ctx contractapi.TransactionContextInterface, recordID string) (int, error) {
	lk, err := ctx.GetStub().CreateCompositeKey(keyLatest, []string{recordID})
	if err != nil {
		return 0, err
	}
	b, err := ctx.GetStub().GetState(lk)
	if err != nil || b == nil {
		return 0, err
	}
	return strconv.Atoi(string(b))
}

func getAnchor(ctx contractapi.TransactionContextInterface, recordID string, version int) (*RecordAnchor, error) {
	rk, err := ctx.GetStub().CreateCompositeKey(keyRecord, []string{recordID, fmt.Sprintf("%06d", version)})
	if err != nil {
		return nil, err
	}
	var a RecordAnchor
	ok, err := getJSON(ctx, rk, &a)
	if err != nil || !ok {
		return nil, err
	}
	return &a, nil
}

func txTime(ctx contractapi.TransactionContextInterface) (string, error) {
	t, err := ctx.GetStub().GetTxTimestamp()
	if err != nil {
		return "", err
	}
	return t.AsTime().UTC().Format(time.RFC3339), nil
}

func putJSON(ctx contractapi.TransactionContextInterface, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return ctx.GetStub().PutState(key, b)
}

func getJSON(ctx contractapi.TransactionContextInterface, key string, v any) (bool, error) {
	b, err := ctx.GetStub().GetState(key)
	if err != nil || b == nil {
		return false, err
	}
	return true, json.Unmarshal(b, v)
}

func count(ctx contractapi.TransactionContextInterface, objType string) (int, error) {
	it, err := ctx.GetStub().GetStateByPartialCompositeKey(objType, []string{})
	if err != nil {
		return 0, err
	}
	defer it.Close()
	n := 0
	for it.HasNext() {
		if _, err := it.Next(); err != nil {
			return 0, err
		}
		n++
	}
	return n, nil
}

func notFound(what, id string, err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%s %s not found", what, id)
}
