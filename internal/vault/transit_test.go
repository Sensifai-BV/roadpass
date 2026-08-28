package vault

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"testing"
)

// Runs against a real OpenBao when ROADPASS_TEST_VAULT_ADDR is set, e.g.
//
//	docker run --rm -d -p 18200:8200 quay.io/openbao/openbao:2.5.1 server -dev -dev-root-token-id=test
//	ROADPASS_TEST_VAULT_ADDR=http://127.0.0.1:18200 ROADPASS_TEST_VAULT_TOKEN=test go test ./internal/vault
func TestTransitAgainstOpenBao(t *testing.T) {
	addr := os.Getenv("ROADPASS_TEST_VAULT_ADDR")
	if addr == "" {
		t.Skip("ROADPASS_TEST_VAULT_ADDR not set")
	}
	ctx := context.Background()
	v, err := New(Config{Addr: addr, Token: os.Getenv("ROADPASS_TEST_VAULT_TOKEN")})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Health(ctx); err != nil {
		t.Fatal(err)
	}
	// Transit may already be mounted from an earlier run.
	_ = v.do(ctx, "POST", "sys/mounts/transit", map[string]any{"type": "transit"}, nil)

	name := "test-" + t.Name()
	if _, err := v.Key(ctx, name+"-missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key: %v", err)
	}
	if err := v.CreateKey(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateKey(ctx, name); err != nil {
		t.Fatalf("creating an existing key must be harmless: %v", err)
	}
	k, err := v.Key(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("roadpass/v1/record\nabc")
	sig, err := v.Sign(ctx, name, k.LatestVersion, msg)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(k.Public[k.LatestVersion], msg, sig) {
		t.Fatal("vault signature does not verify with the vault's public key")
	}
	msgs := make([][]byte, 1203) // more than two batches
	for i := range msgs {
		msgs[i] = []byte{byte(i), byte(i >> 8), 'x'}
	}
	sigs, err := v.SignBatch(ctx, name, k.LatestVersion, msgs)
	if err != nil || len(sigs) != len(msgs) {
		t.Fatalf("batch: %v %d", err, len(sigs))
	}
	for i := range msgs {
		if !ed25519.Verify(k.Public[k.LatestVersion], msgs[i], sigs[i]) {
			t.Fatalf("batch signature %d out of order or invalid", i)
		}
	}
	k2, err := v.Rotate(ctx, name)
	if err != nil || k2.LatestVersion != k.LatestVersion+1 {
		t.Fatalf("rotate: %v %d", err, k2.LatestVersion)
	}
	old, err := v.Sign(ctx, name, k.LatestVersion, msg)
	if err != nil || !ed25519.Verify(k.Public[k.LatestVersion], msg, old) {
		t.Fatalf("signing with a pinned older version: %v", err)
	}
}
