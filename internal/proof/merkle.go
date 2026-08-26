package proof

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// The Merkle construction is RFC 6962-style and must stay identical to
// chaincode/roadpass-proof/merkle.go: leaf = H(0x00||h), node = H(0x01||l||r), and
// an odd last node is promoted unchanged to the next level. Both test suites assert
// the same known-answer root.

// Step is one sibling on the path from a leaf to the root. Left reports whether the
// sibling sits to the left of the running hash.
type Step struct {
	Hash string `json:"hash"`
	Left bool   `json:"left"`
}

// ErrLeaf reports a leaf that is not a SHA-256 hex digest.
var ErrLeaf = errors.New("leaf is not a sha256 hex digest")

func leafNode(b []byte) []byte {
	h := sha256.Sum256(append([]byte{0x00}, b...))
	return h[:]
}

func innerNode(l, r []byte) []byte {
	buf := make([]byte, 0, 1+2*sha256.Size)
	buf = append(append(append(buf, 0x01), l...), r...)
	h := sha256.Sum256(buf)
	return h[:]
}

func decodeLeaves(hexLeaves []string) ([][]byte, error) {
	if len(hexLeaves) == 0 {
		return nil, fmt.Errorf("no leaves")
	}
	level := make([][]byte, len(hexLeaves))
	for i, s := range hexLeaves {
		b, err := hex.DecodeString(s)
		if err != nil || len(b) != sha256.Size {
			return nil, fmt.Errorf("leaf %d: %w", i, ErrLeaf)
		}
		level[i] = leafNode(b)
	}
	return level, nil
}

func nextLevel(level [][]byte) [][]byte {
	next := make([][]byte, 0, (len(level)+1)/2)
	for i := 0; i < len(level); i += 2 {
		if i+1 == len(level) {
			next = append(next, level[i])
			continue
		}
		next = append(next, innerNode(level[i], level[i+1]))
	}
	return next
}

// MerkleRoot returns the hex root over hex SHA-256 leaves.
func MerkleRoot(hexLeaves []string) (string, error) {
	level, err := decodeLeaves(hexLeaves)
	if err != nil {
		return "", err
	}
	for len(level) > 1 {
		level = nextLevel(level)
	}
	return hex.EncodeToString(level[0]), nil
}

// InclusionProof returns the root and the audit path for leaves[index].
func InclusionProof(hexLeaves []string, index int) (string, []Step, error) {
	level, err := decodeLeaves(hexLeaves)
	if err != nil {
		return "", nil, err
	}
	if index < 0 || index >= len(level) {
		return "", nil, fmt.Errorf("index %d out of range [0,%d)", index, len(level))
	}
	var path []Step
	for len(level) > 1 {
		sib := index ^ 1
		if sib < len(level) { // a promoted odd node has no sibling at this level
			path = append(path, Step{Hash: hex.EncodeToString(level[sib]), Left: sib < index})
		}
		level = nextLevel(level)
		index /= 2
	}
	return hex.EncodeToString(level[0]), path, nil
}

// VerifyInclusion recomputes the root from one leaf hash and its audit path.
func VerifyInclusion(leafHex string, path []Step, rootHex string) bool {
	b, err := hex.DecodeString(leafHex)
	if err != nil || len(b) != sha256.Size {
		return false
	}
	cur := leafNode(b)
	for _, s := range path {
		sib, err := hex.DecodeString(s.Hash)
		if err != nil || len(sib) != sha256.Size {
			return false
		}
		if s.Left {
			cur = innerNode(sib, cur)
		} else {
			cur = innerNode(cur, sib)
		}
	}
	return hex.EncodeToString(cur) == rootHex
}
