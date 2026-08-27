// Package keyring hands out signers for companies and members, wherever their
// private keys live: inside OpenBao Transit (never released), or sealed in the
// database under SIGNING_MASTER_KEY (development, and the simulated demo data).
//
// Every member has a personal key. An action they take is signed twice: with
// the company key (the company stands behind it) and with their own key (this
// person did it). Both public keys are registered on the ledger by the worker.
package keyring

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sensifai/roadpass/internal/signing"
	"github.com/sensifai/roadpass/internal/store"
	"github.com/sensifai/roadpass/internal/vault"
)

// Keyring creates, finds and retires keys.
type Keyring struct {
	St      *store.Store
	Local   *signing.Vault // seeds sealed in the database
	Transit *vault.Transit // nil: no vault configured; new keys are sealed locally

	mu    sync.Mutex
	cache map[string]cached // active signer per holder ("org:<id>" / "user:<id>")
}

// cacheTTL bounds how long another replica may keep signing with a key that was
// retired or revoked elsewhere.
const cacheTTL = 30 * time.Second

type cached struct {
	s  signing.Signer
	at time.Time
}

// ErrCustomerHeld is a company that signs its own records: the platform cannot.
var ErrCustomerHeld = errors.New("this organisation holds its own signing key")

// BatchSigner signs many messages in one go (one vault round trip per batch).
type BatchSigner interface {
	signing.Signer
	SignMessages(ctx context.Context, msgs [][]byte) ([][]byte, error)
}

// SignAll signs msgs with s, in one batch when the signer supports it.
func SignAll(ctx context.Context, s signing.Signer, msgs [][]byte) ([][]byte, error) {
	if b, ok := s.(BatchSigner); ok {
		return b.SignMessages(ctx, msgs)
	}
	out := make([][]byte, len(msgs))
	for i, m := range msgs {
		sig, err := s.SignMessage(ctx, m)
		if err != nil {
			return nil, err
		}
		out[i] = sig
	}
	return out, nil
}

// transitSigner signs with one version of a Transit key.
type transitSigner struct {
	id      string
	pub     ed25519.PublicKey
	v       *vault.Transit
	name    string
	version int
}

func (t *transitSigner) KeyID() string                { return t.id }
func (t *transitSigner) PublicKey() ed25519.PublicKey { return t.pub }
func (t *transitSigner) SignMessage(ctx context.Context, msg []byte) ([]byte, error) {
	sig, err := t.v.Sign(ctx, t.name, t.version, msg)
	if err != nil {
		return nil, err
	}
	// Belt and braces: never store a signature the public key does not verify.
	if !ed25519.Verify(t.pub, msg, sig) {
		return nil, fmt.Errorf("vault key %s v%d signed with a different key than the one on record", t.name, t.version)
	}
	return sig, nil
}
func (t *transitSigner) SignMessages(ctx context.Context, msgs [][]byte) ([][]byte, error) {
	sigs, err := t.v.SignBatch(ctx, t.name, t.version, msgs)
	if err != nil {
		return nil, err
	}
	for i := range msgs {
		if !ed25519.Verify(t.pub, msgs[i], sigs[i]) {
			return nil, fmt.Errorf("vault key %s v%d: signature %d does not verify", t.name, t.version, i)
		}
	}
	return sigs, nil
}

func holder(orgID, userID string) string {
	if userID != "" {
		return "user:" + userID
	}
	return "org:" + orgID
}

// vaultName is the Transit key name of a holder. Ids are hex-encoded: OpenBao
// key names must start and end with a word character, and base64url ids can end
// in "-".
func vaultName(orgID, userID string) string {
	if userID != "" {
		return "user-" + hex.EncodeToString([]byte(userID))
	}
	return "org-" + hex.EncodeToString([]byte(orgID))
}

// OrgSigner returns the company key of an organisation.
func (k *Keyring) OrgSigner(ctx context.Context, orgID string) (signing.Signer, error) {
	return k.signer(ctx, orgID, "", false)
}

// UserSigner returns a member's personal key, creating it on first use.
func (k *Keyring) UserSigner(ctx context.Context, orgID, userID string) (signing.Signer, error) {
	return k.signer(ctx, orgID, userID, true)
}

func (k *Keyring) signer(ctx context.Context, orgID, userID string, create bool) (signing.Signer, error) {
	h := holder(orgID, userID)
	k.mu.Lock()
	if c, ok := k.cache[h]; ok && time.Since(c.at) < cacheTTL {
		k.mu.Unlock()
		return c.s, nil
	}
	k.mu.Unlock()
	sk, err := k.St.ActiveKey(ctx, orgID, userID)
	if errors.Is(err, store.ErrNotFound) && create {
		if sk, err = k.Create(ctx, orgID, userID); errors.Is(err, store.ErrConflict) {
			sk, err = k.St.ActiveKey(ctx, orgID, userID) // created concurrently
		}
	}
	if errors.Is(err, store.ErrNotFound) && userID == "" {
		return nil, fmt.Errorf("%w (%s)", ErrCustomerHeld, orgID)
	}
	if err != nil {
		return nil, err
	}
	s, err := k.open(sk)
	if err != nil {
		return nil, err
	}
	k.mu.Lock()
	if k.cache == nil {
		k.cache = map[string]cached{}
	}
	k.cache[h] = cached{s, time.Now()}
	k.mu.Unlock()
	return s, nil
}

func (k *Keyring) open(sk store.SigningKey) (signing.Signer, error) {
	switch sk.Custody {
	case "vault":
		if k.Transit == nil {
			return nil, fmt.Errorf("key %s is held in the vault, but no vault is configured (VAULT_ADDR)", sk.ID)
		}
		return &transitSigner{id: sk.ID, pub: sk.Public, v: k.Transit, name: sk.VaultKey, version: sk.VaultVersion}, nil
	case "platform":
		if k.Local == nil {
			return nil, fmt.Errorf("key %s is sealed locally, but SIGNING_MASTER_KEY is not loaded", sk.ID)
		}
		key, err := k.Local.Open(sk.ID, sk.OrgID, sk.SealedSeed)
		if err != nil {
			return nil, err
		}
		if !key.Public.Equal(sk.Public) {
			return nil, fmt.Errorf("key %s: sealed seed does not match its public key", sk.ID)
		}
		return key, nil
	}
	return nil, fmt.Errorf("%w (%s)", ErrCustomerHeld, sk.ID)
}

// Create issues a new key for a holder: in the vault when one is configured,
// sealed locally otherwise. The caller retires the previous key if any.
func (k *Keyring) Create(ctx context.Context, orgID, userID string) (store.SigningKey, error) {
	n, err := k.St.CountKeys(ctx, orgID, userID)
	if err != nil {
		return store.SigningKey{}, err
	}
	owner := orgID
	if userID != "" {
		owner = userID
	}
	sk := store.SigningKey{ID: signing.KeyID(owner, n+1), OrgID: orgID, UserID: userID}
	if k.Transit != nil {
		name := vaultName(orgID, userID)
		if err := k.Transit.CreateKey(ctx, name); err != nil {
			return sk, err
		}
		info, err := k.Transit.Key(ctx, name)
		if err != nil {
			return sk, err
		}
		// A holder's first vault key uses the key's current version; later keys
		// rotate it, so each key id pins exactly one Transit version.
		version := info.LatestVersion
		if used, err := k.vaultVersionUsed(ctx, name, version); err != nil {
			return sk, err
		} else if used {
			if info, err = k.Transit.Rotate(ctx, name); err != nil {
				return sk, err
			}
			version = info.LatestVersion
		}
		sk.Custody, sk.VaultKey, sk.VaultVersion, sk.Public = "vault", name, version, info.Public[version]
	} else {
		if k.Local == nil {
			return sk, errors.New("no vault and no SIGNING_MASTER_KEY: cannot issue keys")
		}
		key, err := signing.NewKey(sk.ID, orgID, nil)
		if err != nil {
			return sk, err
		}
		sealed, err := k.Local.Seal(key)
		if err != nil {
			return sk, err
		}
		sk.Custody, sk.SealedSeed, sk.Public = "platform", sealed, key.Public
	}
	if err := k.St.InsertSigningKey(ctx, sk); err != nil {
		return sk, err
	}
	return k.St.SigningKey(ctx, sk.ID)
}

func (k *Keyring) vaultVersionUsed(ctx context.Context, name string, version int) (bool, error) {
	var used bool
	err := k.St.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM signing_keys WHERE vault_key=$1 AND vault_version=$2)`, name, version).Scan(&used)
	return used, err
}

// Rotate retires a member's active key and issues a new one. Signatures made
// with the old key stay valid; the worker records the retirement on the ledger.
func (k *Keyring) Rotate(ctx context.Context, orgID, userID string) (store.SigningKey, error) {
	if cur, err := k.St.ActiveKey(ctx, orgID, userID); err == nil {
		if err := k.St.EndKey(ctx, cur.ID, "retired", "rotated"); err != nil {
			return store.SigningKey{}, err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
