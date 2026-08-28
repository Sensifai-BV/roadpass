package vault

import (
	"context"
	"os"
	"testing"
)

func TestKVAgainstOpenBao(t *testing.T) {
	addr := os.Getenv("ROADPASS_TEST_VAULT_ADDR")
	if addr == "" {
		t.Skip("ROADPASS_TEST_VAULT_ADDR not set")
	}
	ctx := context.Background()
	v, _ := New(Config{Addr: addr, Token: os.Getenv("ROADPASS_TEST_VAULT_TOKEN")})
	_ = v.do(ctx, "POST", "sys/mounts/roadpass-kv", map[string]any{"type": "kv", "options": map[string]string{"version": "2"}}, nil)
	kv := v.KV("roadpass-kv")
	if err := kv.Put(ctx, "channels/test", map[string]string{"token": "s3cret"}); err != nil {
		t.Fatal(err)
	}
	got, err := kv.Get(ctx, "channels/test")
	if err != nil || got["token"] != "s3cret" {
		t.Fatalf("get: %v %v", err, got)
	}
	if err := kv.Delete(ctx, "channels/test"); err != nil {
		t.Fatal(err)
	}
	if _, err := kv.Get(ctx, "channels/test"); err == nil {
		t.Fatal("deleted secret still readable")
	}
}
