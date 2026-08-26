package proof

import (
	"encoding/json"
	"testing"
)

func mustMap(t *testing.T, s string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// A record read back from Orion-LD (ms timestamps, sysAttrs, different key order,
// 5000.0 vs 5000) must hash identically to the record we anchored.
func TestHashStableAcrossBrokerRoundTrip(t *testing.T) {
	local := mustMap(t, `{"id":"urn:ngsi-ld:roadpass:Batch:1","type":"Batch","@context":"x",
	  "inputMass":{"type":"Property","value":5000.0,"unitCode":"KGM","observedAt":"2026-03-02T08:00:00Z"}}`)
	remote := mustMap(t, `{"type":"Batch","id":"urn:ngsi-ld:roadpass:Batch:1","createdAt":"2026-09-23T10:00:00.123Z",
	  "inputMass":{"unitCode":"KGM","observedAt":"2026-03-02T08:00:00.000Z","value":5000,"type":"Property","modifiedAt":"x"},
	  "proof":{"type":"Property","value":{"contentHash":"abc"}}}`)
	h1, _ := Hash(local)
	h2, _ := Hash(remote)
	if h1 != h2 {
		t.Fatalf("round trip changed hash: %s vs %s", h1, h2)
	}
	remote["inputMass"].(map[string]any)["value"] = 5001.0
	h3, _ := Hash(remote)
	if h3 == h1 {
		t.Fatal("content change not detected")
	}
}

// Instances of a multi-instance attribute carry platform instanceIds; they must not
// enter the hash, while the instance values must.
func TestHashIgnoresInstanceIDsInMultiInstanceAttribute(t *testing.T) {
	a := mustMap(t, `{"id":"urn:x:1","type":"ExtruderObservation","t":[
	  {"type":"Property","value":22.4,"observedAt":"2026-01-02T09:00:01Z","instanceId":"urn:i:1"},
	  {"type":"Property","value":224.0,"observedAt":"2026-01-02T09:00:01Z","instanceId":"urn:i:2"}]}`)
	b := mustMap(t, `{"id":"urn:x:1","type":"ExtruderObservation","t":[
	  {"type":"Property","value":22.4,"observedAt":"2026-01-02T09:00:01.000Z","instanceId":"urn:i:7"},
	  {"type":"Property","value":224.0,"observedAt":"2026-01-02T09:00:01Z","instanceId":"urn:i:8"}]}`)
	ha, _ := Hash(a)
	hb, _ := Hash(b)
	if ha != hb {
		t.Fatalf("instanceId/observedAt precision changed hash: %s vs %s", ha, hb)
	}
	b["t"].([]any)[1].(map[string]any)["value"] = 22.4
	if hc, _ := Hash(b); hc == ha {
		t.Fatal("instance value change not detected")
	}
}

func TestMerkleMatchesChaincodeVector(t *testing.T) {
	// same construction as chaincode/roadpass-proof/merkle.go
	a := HashBytes([]byte("a"))
	b := HashBytes([]byte("b"))
	c := HashBytes([]byte("c"))
	r1, _ := MerkleRoot([]string{a, b, c})
	r2, _ := MerkleRoot([]string{a, c, b})
	if r1 == r2 || len(r1) != 64 {
		t.Fatal("bad merkle root")
	}
	// known-answer vector shared with chaincode/roadpass-proof/contract_test.go
	if r1 != "cac3d448d4e20a2ad5eae1f500e63c2a7f9217cd14572ba7fd22e26dc1ec2648" {
		t.Fatalf("merkle KAT mismatch: %s", r1)
	}
}

// Orion-LD expands concise sub-properties; the hash must not change.
func TestConciseSubPropertyEqualsNormalised(t *testing.T) {
	a := mustMap(t, `{"id":"x","type":"T","q":{"type":"Property","value":1,"provenance":"PublicReference"}}`)
	b := mustMap(t, `{"id":"x","type":"T","q":{"type":"Property","value":1,"provenance":{"type":"Property","value":"PublicReference"}}}`)
	ha, _ := Hash(a)
	hb, _ := Hash(b)
	if ha != hb {
		t.Fatal("concise vs normalised sub-property must hash identically")
	}
}
