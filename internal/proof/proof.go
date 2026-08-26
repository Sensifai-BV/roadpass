// Package proof implements ROADPASS record hashing and Merkle roots.
//
// contentHash = SHA-256( JCS/RFC 8785 ( normalise(entity) ) )
//
// normalise() removes everything that is not part of the passport content or that
// a context broker may rewrite: "@context", the "proof" attribute itself, NGSI-LD
// system attributes (createdAt, modifiedAt, instanceId, …) and it rewrites every
// observedAt to second-precision UTC. The same function is used when verifying
// data read back from the CIRCULOOS Data Platform, so the round trip verifies.
package proof

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gowebpki/jcs"
)

// Algorithm is recorded in every proof so a verifier knows how to recompute it.
const Algorithm = "sha256/jcs-rfc8785/roadpass-normalise-v1"

var dropTop = map[string]bool{"@context": true, "proof": true, "createdAt": true, "modifiedAt": true,
	"deletedAt": true, "scope": false}
var dropAttr = map[string]bool{"createdAt": true, "modifiedAt": true, "instanceId": true, "deletedAt": true}

// NGSI-LD reserved attribute members; anything else inside an attribute is a
// sub-attribute and is hashed in normalised form ({"type":"Property","value":v}),
// because brokers expand the concise form (Orion-LD does).
var reservedMember = map[string]bool{"type": true, "value": true, "object": true, "unitCode": true,
	"observedAt": true, "datasetId": true, "lang": true, "languageMap": true, "objectType": true,
	"valueList": true, "objectList": true, "json": true, "vocab": true}

// Normalise returns the hashable form of one NGSI-LD entity.
func Normalise(e map[string]any) map[string]any {
	out := make(map[string]any, len(e))
	for k, v := range e {
		if dropTop[k] {
			continue
		}
		switch a := v.(type) {
		case map[string]any:
			out[k] = normAttr(a)
		case []any: // multi-instance attribute (several datasetIds or temporal instances)
			l := make([]any, len(a))
			for i, x := range a {
				if m, ok := x.(map[string]any); ok {
					l[i] = normAttr(m)
				} else {
					l[i] = x
				}
			}
			out[k] = l
		default:
			out[k] = v
		}
	}
	return out
}

func normAttr(a map[string]any) map[string]any {
	o := make(map[string]any, len(a))
	for k, v := range a {
		if dropAttr[k] {
			continue
		}
		if !reservedMember[k] {
			if sub, ok := v.(map[string]any); ok && sub["type"] != nil {
				v = normAttr(sub)
			} else {
				v = map[string]any{"type": "Property", "value": v}
			}
		}
		if k == "observedAt" {
			if s, ok := v.(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
					v = t.UTC().Format("2006-01-02T15:04:05Z")
				}
			}
		}
		o[k] = v
	}
	return o
}

// Canonical returns the RFC 8785 canonical JSON bytes of the normalised entity.
func Canonical(e map[string]any) ([]byte, error) {
	raw, err := json.Marshal(Normalise(e))
	if err != nil {
		return nil, err
	}
	return jcs.Transform(raw)
}

// Hash returns the hex SHA-256 content hash of an entity.
func Hash(e map[string]any) (string, error) {
	c, err := Canonical(e)
	if err != nil {
		return "", fmt.Errorf("canonicalise %v: %w", e["id"], err)
	}
	return HashBytes(c), nil
}

// HashJSON hashes any JSON-marshalable value canonically (packages, reports).
func HashJSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	c, err := jcs.Transform(raw)
	if err != nil {
		return "", err
	}
	return HashBytes(c), nil
}

// HashBytes returns hex SHA-256.
func HashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
