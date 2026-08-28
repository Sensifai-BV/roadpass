// Package secrets keeps the secrets ROADPASS must read back (channel passwords,
// API tokens, webhook signing keys, the Web Push private key): in OpenBao's
// key-value store when a vault is configured, otherwise sealed with AES-GCM in
// the database (development only). Callers keep only a reference.
package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sensifai/roadpass/internal/vault"
	"golang.org/x/crypto/hkdf"
)

// Store saves and loads secrets.
type Store struct {
	KV   *vault.KV     // nil: no vault
	DB   *pgxpool.Pool // for local sealing
	aead cipher.AEAD
}

// New returns a store. master is SIGNING_MASTER_KEY's raw bytes; a separate key
// is derived from it so secrets and signing seeds never share a key.
func New(kv *vault.KV, db *pgxpool.Pool, master []byte) (*Store, error) {
	s := &Store{KV: kv, DB: db}
	if len(master) > 0 {
		key := make([]byte, 32)
		if _, err := hkdf.New(sha256.New, master, nil, []byte("roadpass/secrets/v1")).Read(key); err != nil {
			return nil, err
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		if s.aead, err = cipher.NewGCM(block); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// ErrNotFound is a reference with no secret behind it.
var ErrNotFound = errors.New("secret not found")

// Put stores data under name and returns the reference to keep.
func (s *Store) Put(ctx context.Context, name string, data map[string]string) (string, error) {
	if strings.ContainsAny(name, " .") || name == "" {
		return "", fmt.Errorf("secret name %q is not valid", name)
	}
	if s.KV != nil {
		if err := s.KV.Put(ctx, name, data); err != nil {
			return "", err
		}
		return "vault:" + name, nil
	}
	if s.aead == nil {
		return "", errors.New("no vault and no master key: cannot store secrets")
	}
	plain, _ := json.Marshal(data)
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := s.aead.Seal(nonce, nonce, plain, []byte(name))
	if _, err := s.DB.Exec(ctx, `INSERT INTO local_secrets (id, sealed) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET sealed = EXCLUDED.sealed`, name, sealed); err != nil {
		return "", err
	}
	return "local:" + name, nil
}

// Get loads the secret a reference points to.
func (s *Store) Get(ctx context.Context, ref string) (map[string]string, error) {
	kind, name, ok := strings.Cut(ref, ":")
	if !ok {
		return nil, ErrNotFound
	}
	switch kind {
	case "vault":
		if s.KV == nil {
			return nil, fmt.Errorf("secret %s is in the vault, but no vault is configured", name)
		}
		d, err := s.KV.Get(ctx, name)
		if errors.Is(err, vault.ErrNotFound) {
			return nil, ErrNotFound
		}
		return d, err
	case "local":
		if s.aead == nil {
			return nil, errors.New("no master key to open local secrets")
		}
		var sealed []byte
		if err := s.DB.QueryRow(ctx, `SELECT sealed FROM local_secrets WHERE id=$1`, name).Scan(&sealed); err != nil {
			return nil, ErrNotFound
		}
		ns := s.aead.NonceSize()
		if len(sealed) < ns {
			return nil, errors.New("sealed secret too short")
		}
		plain, err := s.aead.Open(nil, sealed[:ns], sealed[ns:], []byte(name))
		if err != nil {
			return nil, fmt.Errorf("secret %s: cannot decrypt (wrong master key?)", name)
		}
		var d map[string]string
		return d, json.Unmarshal(plain, &d)
	}
	return nil, ErrNotFound
}

// Delete removes a secret.
func (s *Store) Delete(ctx context.Context, ref string) error {
	kind, name, _ := strings.Cut(ref, ":")
	switch kind {
	case "vault":
		if s.KV != nil {
			return s.KV.Delete(ctx, name)
		}
	case "local":
		_, err := s.DB.Exec(ctx, `DELETE FROM local_secrets WHERE id=$1`, name)
		return err
	}
	return nil
}
