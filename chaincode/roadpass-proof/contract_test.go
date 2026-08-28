package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hyperledger/fabric-chaincode-go/v2/pkg/cid"
	"github.com/hyperledger/fabric-chaincode-go/v2/shim"
	"github.com/hyperledger/fabric-contract-api-go/v2/contractapi"
	"github.com/hyperledger/fabric-protos-go-apiv2/ledger/queryresult"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ---- minimal Fabric-like stub: writes become visible only after commit ----

type memStub struct {
	shim.ChaincodeStubInterface
	state   map[string][]byte
	writes  map[string][]byte
	txCount int
}

func newStub() *memStub { return &memStub{state: map[string][]byte{}, writes: map[string][]byte{}} }

func (s *memStub) begin() { s.txCount++; s.writes = map[string][]byte{} }
func (s *memStub) commit() {
	for k, v := range s.writes {
		s.state[k] = v
	}
}
func (s *memStub) GetTxID() string { return fmt.Sprintf("tx%03d", s.txCount) }
func (s *memStub) GetTxTimestamp() (*timestamppb.Timestamp, error) {
	return timestamppb.New(time.Date(2026, 9, 23, 12, 0, s.txCount, 0, time.UTC)), nil
}
func (s *memStub) GetState(k string) ([]byte, error) { return s.state[k], nil } // no read-your-writes
func (s *memStub) PutState(k string, v []byte) error { s.writes[k] = v; return nil }
func (s *memStub) CreateCompositeKey(t string, a []string) (string, error) {
	return shim.CreateCompositeKey(t, a)
}
func (s *memStub) GetStateByPartialCompositeKey(t string, a []string) (shim.StateQueryIteratorInterface, error) {
	prefix, _ := shim.CreateCompositeKey(t, a)
	var keys []string
	for k := range s.state {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	it := &memIter{}
	for _, k := range keys {
		it.kvs = append(it.kvs, &queryresult.KV{Key: k, Value: s.state[k]})
	}
	return it, nil
}

type memIter struct {
	kvs []*queryresult.KV
	i   int
}

func (it *memIter) HasNext() bool                  { return it.i < len(it.kvs) }
func (it *memIter) Next() (*queryresult.KV, error) { it.i++; return it.kvs[it.i-1], nil }
func (it *memIter) Close() error                   { return nil }

type fakeID struct {
	msp  string
	cert *x509.Certificate
}

func (f fakeID) GetID() (string, error)                         { return "id", nil }
func (f fakeID) GetMSPID() (string, error)                      { return f.msp, nil }
func (f fakeID) GetAttributeValue(string) (string, bool, error) { return "", false, nil }
func (f fakeID) AssertAttributeValue(string, string) error      { return nil }
func (f fakeID) GetX509Certificate() (*x509.Certificate, error) { return f.cert, nil }

var _ cid.ClientIdentity = fakeID{}

func mkCert(t *testing.T, cn, ou string) *x509.Certificate {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn, OrganizationalUnit: []string{ou}},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return c
}

type harness struct {
	t    *testing.T
	stub *memStub
	cc   *ProofContract
}

func newHarness(t *testing.T) *harness { return &harness{t: t, stub: newStub(), cc: &ProofContract{}} }

func (h *harness) ctx(msp, ou string) *contractapi.TransactionContext {
	h.stub.begin()
	c := &contractapi.TransactionContext{}
	c.SetStub(h.stub)
	c.SetClientIdentity(fakeID{msp: msp, cert: mkCert(h.t, "user@"+msp, ou)})
	return c
}

func hashOf(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func rec(id string, v int, content, prev string) RecordInput {
	return RecordInput{RecordID: "urn:ngsi-ld:roadpass:Batch:" + id, Version: v, EntityType: "Batch",
		ContentHash: hashOf(content), PrevHash: prev, Provenance: "AnonymisedSynthetic", AssuranceStatus: "pass"}
}

func rootOf(rs []RecordInput) string {
	leaves := make([][]byte, len(rs))
	for i, r := range rs {
		leaves[i], _ = hex.DecodeString(r.ContentHash)
	}
	return hex.EncodeToString(MerkleRoot(leaves))
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

// ---- tests ----

func TestImportBatchVerifyAndTamper(t *testing.T) {
	h := newHarness(t)
	rs := []RecordInput{rec("A", 1, "a", ""), rec("B", 1, "b", ""), rec("C", 1, "c", "")}
	b, err := h.cc.AnchorImportBatch(h.ctx("SensifaiMSP", "client"), "IMPORT-1", rootOf(rs), js(rs))
	if err != nil {
		t.Fatal(err)
	}
	h.stub.commit()
	if b.RecordCount != 3 {
		t.Fatalf("count %d", b.RecordCount)
	}
	v, err := h.cc.Verify(h.ctx("PlasmixMSP", "client"), rs[1].RecordID, 0, rs[1].ContentHash)
	if err != nil || v.Status != "verified" || !v.Match || v.Anchor.MerkleIndex != 1 {
		t.Fatalf("verify: %+v %v", v, err)
	}
	v, _ = h.cc.Verify(h.ctx("PlasmixMSP", "client"), rs[1].RecordID, 0, hashOf("tampered"))
	if v.Status != "mismatch" || v.Match {
		t.Fatalf("tamper not detected: %+v", v)
	}
	v, _ = h.cc.Verify(h.ctx("PlasmixMSP", "client"), "urn:ngsi-ld:roadpass:Batch:ZZZ", 0, rs[1].ContentHash)
	if v.Status != "not_anchored" {
		t.Fatalf("expected not_anchored: %+v", v)
	}
	st, _ := h.cc.Stats(h.ctx("SensifaiMSP", "client"))
	if st.RecordVersions != 3 || st.DistinctRecords != 3 || st.ImportBatches != 1 {
		t.Fatalf("stats %+v", st)
	}
}

func TestMerkleRootRecomputedOnChain(t *testing.T) {
	h := newHarness(t)
	rs := []RecordInput{rec("A", 1, "a", ""), rec("B", 1, "b", "")}
	if _, err := h.cc.AnchorImportBatch(h.ctx("SensifaiMSP", "client"), "I", hashOf("wrong"), js(rs)); err == nil ||
		!strings.Contains(err.Error(), "merkle root mismatch") {
		t.Fatalf("expected merkle mismatch, got %v", err)
	}
}

func TestDuplicateInSameBatchRejected(t *testing.T) {
	h := newHarness(t)
	rs := []RecordInput{rec("A", 1, "a", ""), rec("A", 1, "a2", "")}
	if _, err := h.cc.AnchorImportBatch(h.ctx("SensifaiMSP", "client"), "I", rootOf(rs), js(rs)); err == nil {
		t.Fatal("duplicate id in one import must be rejected")
	}
}

func TestVersionChainAndImmutability(t *testing.T) {
	h := newHarness(t)
	r1 := rec("A", 1, "a", "")
	if _, err := h.cc.AnchorRecord(h.ctx("SensifaiMSP", "client"), js(r1)); err != nil {
		t.Fatal(err)
	}
	h.stub.commit()
	// re-anchoring v1 is refused
	if _, err := h.cc.AnchorRecord(h.ctx("SensifaiMSP", "client"), js(r1)); err == nil {
		t.Fatal("v1 overwrite must be refused")
	}
	// v2 with wrong prevHash refused
	if _, err := h.cc.AnchorRecord(h.ctx("SensifaiMSP", "client"), js(rec("A", 2, "a-corrected", hashOf("x")))); err == nil {
		t.Fatal("broken hash chain must be refused")
	}
	// correct v2
	r2 := rec("A", 2, "a-corrected", r1.ContentHash)
	if _, err := h.cc.AnchorRecord(h.ctx("PlasmixMSP", "client"), js(r2)); err != nil {
		t.Fatal(err)
	}
	h.stub.commit()
	hist, _ := h.cc.History(h.ctx("SensifaiMSP", "client"), r1.RecordID)
	if len(hist) != 2 || hist[1].PrevHash != hist[0].ContentHash || hist[1].SubmitterMSP != "PlasmixMSP" {
		t.Fatalf("history %+v", hist)
	}
	v, _ := h.cc.Verify(h.ctx("SensifaiMSP", "client"), r1.RecordID, 1, r1.ContentHash)
	if !v.Match || v.IsLatestVer {
		t.Fatalf("old version should verify but not be latest: %+v", v)
	}
}

func TestOnlyApprovedRecordsAnchored(t *testing.T) {
	h := newHarness(t)
	r := rec("A", 1, "a", "")
	r.AssuranceStatus = "flagged"
	if _, err := h.cc.AnchorRecord(h.ctx("SensifaiMSP", "client"), js(r)); err == nil {
		t.Fatal("flagged record must not be anchored")
	}
	r.AssuranceStatus, r.Provenance = "pass", "Invented"
	if _, err := h.cc.AnchorRecord(h.ctx("SensifaiMSP", "client"), js(r)); err == nil {
		t.Fatal("unknown provenance must be refused")
	}
}

func TestRBAC(t *testing.T) {
	h := newHarness(t)
	rs := []RecordInput{rec("A", 1, "a", "")}
	if _, err := h.cc.AnchorImportBatch(h.ctx("PlasmixMSP", "client"), "I", rootOf(rs), js(rs)); err == nil ||
		!strings.Contains(err.Error(), "access denied") {
		t.Fatalf("non-operator import must be denied, got %v", err)
	}
	if _, err := h.cc.AnchorRecord(h.ctx("SensifaiMSP", "peer"), js(rs[0])); err == nil ||
		!strings.Contains(err.Error(), "access denied") {
		t.Fatalf("peer identity must be denied, got %v", err)
	}
}

func TestEvidencePackage(t *testing.T) {
	h := newHarness(t)
	b := rec("B1", 1, "batch", "")
	e := RecordInput{RecordID: "urn:ngsi-ld:roadpass:ProcessingEvent:P1", Version: 1, EntityType: "ProcessingEvent",
		ContentHash: hashOf("proc"), Provenance: "Operational", AssuranceStatus: "pass"}
	rs := []RecordInput{b, e}
	if _, err := h.cc.AnchorImportBatch(h.ctx("SensifaiMSP", "client"), "I", rootOf(rs), js(rs)); err != nil {
		t.Fatal(err)
	}
	h.stub.commit()
	pkg := EvidencePackageInput{PackageID: "PKG-B1", LDPEBatchID: b.RecordID, PackageHash: hashOf("pkg"),
		AssuranceResultHash: hashOf("ar"), Members: []RecordRef{{b.RecordID, 1}, {e.RecordID, 1}}}
	if _, err := h.cc.AnchorEvidencePackage(h.ctx("PlasmixMSP", "client"), js(pkg)); err == nil {
		t.Fatal("non-operator package must be denied")
	}
	if _, err := h.cc.AnchorEvidencePackage(h.ctx("SensifaiMSP", "client"), js(pkg)); err != nil {
		t.Fatal(err)
	}
	h.stub.commit()
	if _, err := h.cc.AnchorEvidencePackage(h.ctx("SensifaiMSP", "client"), js(pkg)); err == nil {
		t.Fatal("package overwrite must be refused")
	}
	bad := pkg
	bad.PackageID, bad.Members = "PKG-X", []RecordRef{{b.RecordID, 1}, {"urn:ngsi-ld:roadpass:X:none", 1}}
	if _, err := h.cc.AnchorEvidencePackage(h.ctx("SensifaiMSP", "client"), js(bad)); err == nil {
		t.Fatal("package with un-anchored member must be refused")
	}
	got, err := h.cc.GetEvidencePackage(h.ctx("SensifaiMSP", "client"), "PKG-B1")
	if err != nil || got.PackageHash != pkg.PackageHash {
		t.Fatalf("get package: %+v %v", got, err)
	}
}

func TestMerkleOddLevels(t *testing.T) {
	l := func(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }
	one := MerkleRoot([][]byte{l("a")})
	leaf := sha256.Sum256(append([]byte{0}, l("a")...))
	if hex.EncodeToString(one) != hex.EncodeToString(leaf[:]) {
		t.Fatal("single-leaf root must equal leaf hash")
	}
	r3 := MerkleRoot([][]byte{l("a"), l("b"), l("c")})
	r3b := MerkleRoot([][]byte{l("a"), l("c"), l("b")})
	if hex.EncodeToString(r3) == hex.EncodeToString(r3b) {
		t.Fatal("order must matter")
	}
	// known-answer vector shared with cli/internal/proof/proof_test.go
	if got := hex.EncodeToString(r3); got != "cac3d448d4e20a2ad5eae1f500e63c2a7f9217cd14572ba7fd22e26dc1ec2648" {
		t.Fatalf("merkle KAT mismatch: %s", got)
	}
}
