package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func leavesN(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = hashOf(fmt.Sprintf("leaf-%d", i))
	}
	return out
}

func sealIn(id string, leaves []string) SealInput {
	return SealInput{SealID: id, MerkleRoot: rootOfHex(leaves), Leaves: leaves, FirstSeq: 1, LastSeq: int64(len(leaves)), Provenance: "Simulated"}
}

func rootOfHex(ls []string) string {
	rs := make([]RecordInput, len(ls))
	for i, l := range ls {
		rs[i] = RecordInput{ContentHash: l}
	}
	return rootOf(rs)
}

func TestSealBatch(t *testing.T) {
	h := newHarness(t)
	in := sealIn("SEAL-000001", leavesN(5))
	s, err := h.cc.SealBatch(h.ctx("SensifaiMSP", "client"), js(in))
	if err != nil {
		t.Fatal(err)
	}
	h.stub.commit()
	if s.LeafCount != 5 || s.MerkleRoot != in.MerkleRoot || s.SubmitterMSP != "SensifaiMSP" {
		t.Fatalf("unexpected seal %+v", s)
	}
	got, err := h.cc.GetSeal(h.ctx("PlasmixMSP", "client"), "SEAL-000001")
	if err != nil || got.MerkleRoot != in.MerkleRoot {
		t.Fatalf("GetSeal: %v %+v", err, got)
	}
	if _, err := h.cc.SealBatch(h.ctx("SensifaiMSP", "client"), js(in)); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a seal id must be usable once, got %v", err)
	}
}

func TestSealRejects(t *testing.T) {
	h := newHarness(t)
	bad := sealIn("SEAL-2", leavesN(3))
	bad.MerkleRoot = hashOf("forged")
	cases := map[string]struct {
		msp string
		in  SealInput
		msg string
	}{
		"forged root":    {"SensifaiMSP", bad, "merkle root mismatch"},
		"not operator":   {"PlasmixMSP", sealIn("SEAL-3", leavesN(2)), "access denied"},
		"too many":       {"SensifaiMSP", sealIn("SEAL-4", leavesN(MaxSealLeaves+1)), "maximum"},
		"bad id":         {"SensifaiMSP", sealIn("bad id!", leavesN(2)), "valid identifier"},
		"bad provenance": {"SensifaiMSP", func() SealInput { s := sealIn("SEAL-5", leavesN(2)); s.Provenance = "Made-up"; return s }(), "provenance"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := h.cc.SealBatch(h.ctx(c.msp, "client"), js(c.in))
			if err == nil || !strings.Contains(err.Error(), c.msg) {
				t.Fatalf("want error containing %q, got %v", c.msg, err)
			}
		})
	}
}

func TestSignerLifecycle(t *testing.T) {
	h := newHarness(t)
	in := SignerInput{KeyID: "org-1:k1", OrgID: "org-1", OrgName: "Demo", Algorithm: "Ed25519",
		PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}
	if _, err := h.cc.RegisterSigner(h.ctx("PlasmixMSP", "client"), js(in)); err == nil {
		t.Fatal("only the operator may register signers")
	}
	if _, err := h.cc.RegisterSigner(h.ctx("SensifaiMSP", "client"), js(in)); err != nil {
		t.Fatal(err)
	}
	h.stub.commit()
	if _, err := h.cc.RegisterSigner(h.ctx("SensifaiMSP", "client"), js(in)); err == nil {
		t.Fatal("a key id is registered once")
	}
	if _, err := h.cc.RevokeSigner(h.ctx("SensifaiMSP", "client"), "org-1:k1", ""); err == nil {
		t.Fatal("revocation needs a reason")
	}
	s, err := h.cc.RevokeSigner(h.ctx("SensifaiMSP", "client"), "org-1:k1", "key rotated")
	if err != nil || s.Status != "revoked" {
		t.Fatalf("revoke: %v %+v", err, s)
	}
	h.stub.commit()
	got, _ := h.cc.GetSigner(h.ctx("PlasmixMSP", "client"), "org-1:k1")
	if got.Status != "revoked" || got.RevokedAt == "" {
		t.Fatalf("revocation not stored: %+v", got)
	}
	short := in
	short.KeyID, short.PublicKey = "org-1:k2", base64.StdEncoding.EncodeToString(make([]byte, 31))
	if _, err := h.cc.RegisterSigner(h.ctx("SensifaiMSP", "client"), js(short)); err == nil {
		t.Fatal("a 31-byte key must be rejected")
	}
}

func TestPersonalSignerCarriesOnlyAPseudonymousHolder(t *testing.T) {
	h := newHarness(t)
	in := SignerInput{KeyID: "usr_abc:k1", OrgID: "org-1", OrgName: "Demo", Algorithm: "Ed25519", HolderID: "usr_abc",
		PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}
	if _, err := h.cc.RegisterSigner(h.ctx("SensifaiMSP", "client"), js(in)); err != nil {
		t.Fatal(err)
	}
	h.stub.commit()
	got, err := h.cc.GetSigner(h.ctx("PlasmixMSP", "client"), "usr_abc:k1")
	if err != nil || got.HolderID != "usr_abc" || got.OrgID != "org-1" {
		t.Fatalf("personal signer not stored: %v %+v", err, got)
	}
	bad := in
	bad.KeyID, bad.HolderID = "usr_x:k1", "Ana García <ana@example.com>"
	if _, err := h.cc.RegisterSigner(h.ctx("SensifaiMSP", "client"), js(bad)); err == nil {
		t.Fatal("a holder must be an opaque id, not personal data")
	}
}
