package signing

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

const h = "cac3d448d4e20a2ad5eae1f500e63c2a7f9217cd14572ba7fd22e26dc1ec2648"

func TestSignVerify(t *testing.T) {
	k, err := NewKey("org_a:k1", "org_a", nil)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := k.Sign(h)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(k.Public, h, sig); err != nil {
		t.Fatal(err)
	}
	if Verify(k.Public, strings.Replace(h, "c", "d", 1), sig) == nil {
		t.Fatal("signature verified for another hash")
	}
	other, _ := NewKey("org_b:k1", "org_b", nil)
	if Verify(other.Public, h, sig) == nil {
		t.Fatal("signature verified with another key")
	}
}

func TestVaultRoundTripAndBinding(t *testing.T) {
	v, err := NewVault(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	k, _ := NewKey("org_a:k1", "org_a", nil)
	sealed, err := v.Seal(k)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Open("org_a:k1", "org_a", sealed)
	if err != nil || !bytes.Equal(got.Public, k.Public) {
		t.Fatalf("round trip: %v", err)
	}
	if _, err := v.Open("org_b:k1", "org_b", sealed); err == nil {
		t.Fatal("ciphertext opened under another key id")
	}
	if _, err := NewVault("short"); err == nil {
		t.Fatal("short master key accepted")
	}
}

// The browser (web/src/lib/verify.test.ts) builds the same bytes; keep in sync.
func TestReceiptMessageV2Vector(t *testing.T) {
	at := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	got := string(ReceiptMessage("ab12", ReceiptFacts{ReceivedAt: at, MassKg: 18250.5, Decision: "accepted_with_note", Note: "Wet bales"}))
	want := "roadpass/v2/receipt\nab12\n2026-10-15T10:00:00Z\n18250500\naccepted_with_note\n" +
		"309dafaa7affcf9af73d9f7b67e5b76a8a34d073e43c122bec1a4afb8d974ded" // sha256("Wet bales")
	if got != want {
		t.Fatalf("v2 receipt bytes changed:\n%q", got)
	}
	if v1 := string(ReceiptMessage("ab12", ReceiptFacts{ReceivedAt: at, MassKg: 18250.5})); v1 != "roadpass/v1/receipt\nab12\n2026-10-15T10:00:00Z\n18250500" {
		t.Fatalf("v1 receipts must keep their exact bytes: %q", v1)
	}
}
