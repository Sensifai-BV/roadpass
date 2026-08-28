// Package vault talks to OpenBao's Transit engine (API-compatible with HashiCorp
// Vault). Signing keys are created there as non-exportable Ed25519 keys; the
// service asks the vault to sign and never holds a private key.
package vault

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Transit is a client for one Transit mount.
type Transit struct {
	addr, mount string
	tokenFile   string
	http        *http.Client

	mu    sync.RWMutex
	token string
}

// Config configures the client. Token or TokenFile (re-read when the vault says
// the token is no longer valid, so it can be rotated without a restart).
type Config struct {
	Addr      string // https://openbao:8200
	Mount     string // "transit"
	Token     string
	TokenFile string
	Timeout   time.Duration
}

// New returns a client; it does not contact the vault.
func New(c Config) (*Transit, error) {
	if c.Addr == "" {
		return nil, errors.New("vault: address is required")
	}
	if c.Mount == "" {
		c.Mount = "transit"
	}
	if c.Timeout == 0 {
		c.Timeout = 10 * time.Second
	}
	t := &Transit{addr: strings.TrimRight(c.Addr, "/"), mount: strings.Trim(c.Mount, "/"), tokenFile: c.TokenFile,
		token: c.Token, http: &http.Client{Timeout: c.Timeout}}
	if t.token == "" && t.tokenFile == "" {
		return nil, errors.New("vault: a token or a token file is required")
	}
	return t, nil
}

// ErrNotFound is a key that does not exist.
var ErrNotFound = errors.New("vault: key not found")

// APIError is a non-success answer from the vault.
type APIError struct {
	Status int
	Errors []string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("vault: HTTP %d: %s", e.Status, strings.Join(e.Errors, "; "))
}

func (t *Transit) currentToken() (string, error) {
	t.mu.RLock()
	tok := t.token
	t.mu.RUnlock()
	if tok != "" {
		return tok, nil
	}
	return t.reloadToken()
}

func (t *Transit) reloadToken() (string, error) {
	if t.tokenFile == "" {
		return t.token, nil
	}
	b, err := os.ReadFile(t.tokenFile)
	if err != nil {
		return "", fmt.Errorf("vault: read token file: %w", err)
	}
	tok := strings.TrimSpace(string(b))
	t.mu.Lock()
	t.token = tok
	t.mu.Unlock()
	return tok, nil
}

// do sends one request; on 403 with a token file it re-reads the token once.
func (t *Transit) do(ctx context.Context, method, path string, in, out any) error {
	for attempt := 0; ; attempt++ {
		tok, err := t.currentToken()
		if err != nil {
			return err
		}
		var body io.Reader
		if in != nil {
			b, err := json.Marshal(in)
			if err != nil {
				return err
			}
			body = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, t.addr+"/v1/"+path, body)
		if err != nil {
			return err
		}
		req.Header.Set("X-Vault-Token", tok)
		if in != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := t.http.Do(req)
		if err != nil {
			return fmt.Errorf("vault: %w", err)
		}
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if res.StatusCode == http.StatusForbidden && t.tokenFile != "" && attempt == 0 {
			if _, err := t.reloadToken(); err == nil {
				continue
			}
		}
		if res.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		if res.StatusCode >= 300 {
			var e struct {
				Errors []string `json:"errors"`
			}
			_ = json.Unmarshal(raw, &e)
			return &APIError{Status: res.StatusCode, Errors: e.Errors}
		}
		if out != nil && len(raw) > 0 {
			return json.Unmarshal(raw, out)
		}
		return nil
	}
}

// Health reports whether the vault is initialised, unsealed and reachable.
func (t *Transit) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.addr+"/v1/sys/health", nil)
	if err != nil {
		return err
	}
	res, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	res.Body.Close()
	switch res.StatusCode {
	case 200, 429, 473: // active, standby, performance standby
		return nil
	case 503:
		return errors.New("vault: sealed")
	case 501:
		return errors.New("vault: not initialised")
	}
	return fmt.Errorf("vault: health HTTP %d", res.StatusCode)
}

// KeyInfo is a Transit key's public half per version.
type KeyInfo struct {
	Name          string
	LatestVersion int
	Public        map[int]ed25519.PublicKey
}

// CreateKey creates a non-exportable Ed25519 key; an existing key is left alone.
func (t *Transit) CreateKey(ctx context.Context, name string) error {
	err := t.do(ctx, http.MethodPost, t.mount+"/keys/"+url.PathEscape(name),
		map[string]any{"type": "ed25519", "exportable": false, "allow_plaintext_backup": false}, nil)
	if errors.Is(err, ErrNotFound) {
		return fmt.Errorf("vault: cannot create key %q: invalid name, or Transit is not mounted at %q", name, t.mount)
	}
	return err
}

// Key reads a key's public halves.
func (t *Transit) Key(ctx context.Context, name string) (KeyInfo, error) {
	var out struct {
		Data struct {
			Name          string `json:"name"`
			Type          string `json:"type"`
			LatestVersion int    `json:"latest_version"`
			Keys          map[string]struct {
				PublicKey string `json:"public_key"`
			} `json:"keys"`
		} `json:"data"`
	}
	if err := t.do(ctx, http.MethodGet, t.mount+"/keys/"+url.PathEscape(name), nil, &out); err != nil {
		return KeyInfo{}, err
	}
	if out.Data.Type != "ed25519" {
		return KeyInfo{}, fmt.Errorf("vault: key %s is %s, not ed25519", name, out.Data.Type)
	}
	info := KeyInfo{Name: out.Data.Name, LatestVersion: out.Data.LatestVersion, Public: map[int]ed25519.PublicKey{}}
	for v, k := range out.Data.Keys {
		n, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		pub, err := base64.StdEncoding.DecodeString(k.PublicKey)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			return KeyInfo{}, fmt.Errorf("vault: key %s v%d: bad public key", name, n)
		}
		info.Public[n] = pub
	}
	return info, nil
}

// Rotate adds a new version to a key and returns it.
func (t *Transit) Rotate(ctx context.Context, name string) (KeyInfo, error) {
	if err := t.do(ctx, http.MethodPost, t.mount+"/keys/"+url.PathEscape(name)+"/rotate", map[string]any{}, nil); err != nil {
		return KeyInfo{}, err
	}
	return t.Key(ctx, name)
}

// Sign signs msg with one version of a key (Ed25519 signs the message itself, no
// pre-hash) and returns the raw 64-byte signature.
func (t *Transit) Sign(ctx context.Context, name string, version int, msg []byte) ([]byte, error) {
	var out struct {
		Data struct {
			Signature string `json:"signature"`
		} `json:"data"`
	}
	in := map[string]any{"input": base64.StdEncoding.EncodeToString(msg), "key_version": version}
	if err := t.do(ctx, http.MethodPost, t.mount+"/sign/"+url.PathEscape(name), in, &out); err != nil {
		return nil, err
	}
	return parseSig(out.Data.Signature, version) // "vault:v<version>:<base64>"
}

// MaxBatch is how many messages SignBatch sends per request: OpenBao's default
// listener limits on JSON size reject batches somewhere between 200 and 500.
const MaxBatch = 200

// SignBatch signs many messages with one key version in as few requests as
// possible; signatures come back in input order.
func (t *Transit) SignBatch(ctx context.Context, name string, version int, msgs [][]byte) ([][]byte, error) {
	out := make([][]byte, 0, len(msgs))
	for start := 0; start < len(msgs); start += MaxBatch {
		chunk := msgs[start:min(start+MaxBatch, len(msgs))]
		in := make([]map[string]string, len(chunk))
		for i, m := range chunk {
			in[i] = map[string]string{"input": base64.StdEncoding.EncodeToString(m)}
		}
		var res struct {
			Data struct {
				BatchResults []struct {
					Signature string `json:"signature"`
					Error     string `json:"error"`
				} `json:"batch_results"`
			} `json:"data"`
		}
		if err := t.do(ctx, http.MethodPost, t.mount+"/sign/"+url.PathEscape(name),
			map[string]any{"batch_input": in, "key_version": version}, &res); err != nil {
			return nil, err
		}
		if len(res.Data.BatchResults) != len(chunk) {
			return nil, fmt.Errorf("vault: %d signatures for %d messages", len(res.Data.BatchResults), len(chunk))
		}
		for i, r := range res.Data.BatchResults {
			if r.Error != "" {
				return nil, fmt.Errorf("vault: message %d: %s", start+i, r.Error)
			}
			sig, err := parseSig(r.Signature, version)
			if err != nil {
				return nil, err
			}
			out = append(out, sig)
		}
	}
	return out, nil
}

func parseSig(s string, version int) ([]byte, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 || parts[0] != "vault" || parts[1] != "v"+strconv.Itoa(version) {
		return nil, fmt.Errorf("vault: unexpected signature format %q", s)
	}
	sig, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("vault: signature is not 64 bytes")
	}
	return sig, nil
}

// KV is OpenBao's key-value store (version 2) at one mount, for secrets the
// service must read back (channel passwords, API tokens, signing secrets).
type KV struct {
	t     *Transit
	mount string
}

// KV returns a client for the key-value mount, sharing this client's token.
func (t *Transit) KV(mount string) *KV { return &KV{t: t, mount: strings.Trim(mount, "/")} }

// Put stores (a new version of) the secret at path.
func (k *KV) Put(ctx context.Context, path string, data map[string]string) error {
	return k.t.do(ctx, http.MethodPost, k.mount+"/data/"+path, map[string]any{"data": data}, nil)
}

// Get reads the latest version of the secret at path.
func (k *KV) Get(ctx context.Context, path string) (map[string]string, error) {
	var out struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := k.t.do(ctx, http.MethodGet, k.mount+"/data/"+path, nil, &out); err != nil {
		return nil, err
	}
	return out.Data.Data, nil
}

// Delete removes the secret and all its versions.
func (k *KV) Delete(ctx context.Context, path string) error {
	err := k.t.do(ctx, http.MethodDelete, k.mount+"/metadata/"+path, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
