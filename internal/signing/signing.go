// Package signing holds organisation signing keys and record signatures.
//
// Every organisation has an Ed25519 key. It signs SigningMessage(contentHash); the
// public key is registered on the ledger (RegisterSigner), so a verifier needs only
// the ledger and the record. Private keys are stored encrypted with AES-256-GCM
// under the platform master key; the key id is the additional authenticated data,
// so a ciphertext cannot be moved to another key's row.
package signing

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

// Domain separates record signatures from any other use of the same key.
const Domain = "roadpass/v1/record\n"

// Algorithm is the only supported signature algorithm.
const Algorithm = "Ed25519"

// ErrBadSignature is returned when a signature does not verify.
var ErrBadSignature = errors.New("signature does not verify")

// SigningMessage is the exact byte string that is signed for a content hash.
func SigningMessage(contentHash string) []byte { return []byte(Domain + contentHash) }

// ReceiptDomain separates shipment receipts from record signatures. v1 receipts
// confirm time and mass; v2 adds the receiver's intake decision and a digest of
// their note.
const (
	ReceiptDomain   = "roadpass/v1/receipt\n"
	ReceiptDomainV2 = "roadpass/v2/receipt\n"
)

// ReceiptFacts is what a receiver signs about a shipment.
type ReceiptFacts struct {
	ReceivedAt time.Time
	MassKg     float64
	Decision   string // "" = a v1 receipt
	Note       string
}

// ReceiptMessage is the exact byte string a receipt signature covers.
func ReceiptMessage(contentHash string, f ReceiptFacts) []byte {
	at := f.ReceivedAt.UTC().Truncate(time.Second).Format(time.RFC3339)
	grams := int64(math.Round(f.MassKg * 1000))
	if f.Decision == "" {
		return fmt.Appendf(nil, "%s%s\n%s\n%d", ReceiptDomain, contentHash, at, grams)
	}
	note := sha256.Sum256([]byte(f.Note))
	return fmt.Appendf(nil, "%s%s\n%s\n%d\n%s\n%x", ReceiptDomainV2, contentHash, at, grams, f.Decision, note)
}

// SignReceipt signs a receipt.
func (k *Key) SignReceipt(contentHash string, f ReceiptFacts) ([]byte, error) {
	if !k.CanSign() {
		return nil, fmt.Errorf("key %s: private key not loaded", k.ID)
	}
	return ed25519.Sign(k.private, ReceiptMessage(contentHash, f)), nil
}

// VerifyReceipt checks a receipt signature.
func VerifyReceipt(pub ed25519.PublicKey, contentHash string, f ReceiptFacts, sig []byte) error {
	if len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize ||
		!ed25519.Verify(pub, ReceiptMessage(contentHash, f), sig) {
		return ErrBadSignature
	}
	return nil
}

// Signer signs with one key wherever its private half lives: in this process
// (Key), or inside a vault that never releases it.
type Signer interface {
	KeyID() string
	PublicKey() ed25519.PublicKey
	SignMessage(ctx context.Context, msg []byte) ([]byte, error)
}

// SignRecord signs a record's content hash with any signer.
func SignRecord(ctx context.Context, s Signer, contentHash string) ([]byte, error) {
	return s.SignMessage(ctx, SigningMessage(contentHash))
}

// SignReceiptWith signs a receipt with any signer.
func SignReceiptWith(ctx context.Context, s Signer, contentHash string, f ReceiptFacts) ([]byte, error) {
	return s.SignMessage(ctx, ReceiptMessage(contentHash, f))
}

// KeyID implements Signer.
func (k *Key) KeyID() string { return k.ID }

// PublicKey implements Signer.
func (k *Key) PublicKey() ed25519.PublicKey { return k.Public }

// SignMessage implements Signer.
func (k *Key) SignMessage(_ context.Context, msg []byte) ([]byte, error) {
	if !k.CanSign() {
		return nil, fmt.Errorf("key %s: private key not loaded", k.ID)
	}
	return ed25519.Sign(k.private, msg), nil
}

// Key is one signing key held in this process (an organisation's or a person's).
type Key struct {
	ID      string
	OrgID   string
	Public  ed25519.PublicKey
	private ed25519.PrivateKey
}

// KeyID returns the conventional id of an organisation's n-th key.
func KeyID(orgID string, n int) string { return fmt.Sprintf("%s:k%d", orgID, n) }

// NewKey generates a key for an organisation.
func NewKey(id, orgID string, rnd io.Reader) (*Key, error) {
	if rnd == nil {
		rnd = rand.Reader
	}
	pub, priv, err := ed25519.GenerateKey(rnd)
	if err != nil {
		return nil, err
	}
	return &Key{ID: id, OrgID: orgID, Public: pub, private: priv}, nil
}

// KeyFromSeed rebuilds a key from its 32-byte seed (simulation, tests).
func KeyFromSeed(id, orgID string, seed []byte) *Key {
	priv := ed25519.NewKeyFromSeed(seed)
	return &Key{ID: id, OrgID: orgID, Public: priv.Public().(ed25519.PublicKey), private: priv}
}

// CanSign reports whether the private half is loaded.
func (k *Key) CanSign() bool { return len(k.private) == ed25519.PrivateKeySize }

// Sign signs a content hash.
func (k *Key) Sign(contentHash string) ([]byte, error) {
	if !k.CanSign() {
		return nil, fmt.Errorf("key %s: private key not loaded", k.ID)
	}
	return ed25519.Sign(k.private, SigningMessage(contentHash)), nil
}

// PublicBase64 is the encoding registered on the ledger.
func (k *Key) PublicBase64() string { return base64.StdEncoding.EncodeToString(k.Public) }

// Verify checks a signature over a content hash with a public key.
func Verify(pub ed25519.PublicKey, contentHash string, sig []byte) error {
	if len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return ErrBadSignature
	}
	if !ed25519.Verify(pub, SigningMessage(contentHash), sig) {
		return ErrBadSignature
	}
	return nil
}

// Vault encrypts private keys at rest.
type Vault struct{ aead cipher.AEAD }

// NewVault takes the 32-byte master key as hex or base64.
func NewVault(master string) (*Vault, error) {
	raw, err := MasterKey(master)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
