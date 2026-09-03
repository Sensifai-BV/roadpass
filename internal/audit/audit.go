// Package audit appends one JSON line per write the CLI makes — Fabric transactions
// (committed or refused), channel config updates and CIRCULOOS platform upserts — to an
// append-only JSONL file (KPI 13 logging). Entries carry ids, hashes and outcomes only:
// no business data, no credentials.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Entry kinds.
const (
	LedgerSubmit   = "ledger.submit"
	ChannelConfig  = "channel.config_update"
	PlatformUpsert = "platform.upsert"
	PlatformDelete = "platform.delete"
)

// Entry is one audited write.
type Entry struct {
	At            string   `json:"at"`
	Kind          string   `json:"kind"`
	Actor         string   `json:"actor"`             // MSP id or platform account
	Target        string   `json:"target"`            // channel/chaincode or platform tenant
	Op            string   `json:"op"`                // transaction name or HTTP operation
	Subject       string   `json:"subject,omitempty"` // record, batch or package id
	IDs           []string `json:"ids,omitempty"`     // entity ids of a platform upsert
	PayloadSHA256 string   `json:"payloadSha256,omitempty"`
	TxID          string   `json:"txId,omitempty"`
	HTTP          int      `json:"http,omitempty"`
	Outcome       string   `json:"outcome"` // committed | rejected | accepted | failed
	Detail        string   `json:"detail,omitempty"`
	Seconds       float64  `json:"seconds"`
}

// Log is an append-only JSONL audit file. A nil *Log discards entries.
type Log struct{ path string }

// Open returns the audit log at path (created on first append).
func Open(path string) *Log { return &Log{path} }

// Append writes e as one line, stamping At if it is empty.
func (l *Log) Append(e Entry) error {
	if l == nil {
		return nil
	}
	if e.At == "" {
		e.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("audit log: %w", err)
	}
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("audit log: %w", err)
	}
	return nil
}

// SHA256 fingerprints a payload so an entry can be matched to what was sent.
func SHA256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
