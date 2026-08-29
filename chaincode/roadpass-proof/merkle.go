package main

import "crypto/sha256"

// MerkleRoot computes an RFC 6962-style root over hex-decoded SHA-256 leaves:
// leaf = H(0x00 || h), node = H(0x01 || left || right); an odd last node is
// promoted unchanged to the next level. Returns hex. Empty input -> "".
func MerkleRoot(leaves [][]byte) []byte {
	if len(leaves) == 0 {
		return nil
	}
	level := make([][]byte, len(leaves))
	for i, l := range leaves {
		h := sha256.Sum256(append([]byte{0x00}, l...))
		level[i] = h[:]
	}
	for len(level) > 1 {
		next := make([][]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			if i+1 == len(level) {
				next = append(next, level[i])
				continue
			}
			buf := make([]byte, 0, 1+2*sha256.Size)
			buf = append(buf, 0x01)
			buf = append(buf, level[i]...)
			buf = append(buf, level[i+1]...)
			h := sha256.Sum256(buf)
			next = append(next, h[:])
		}
		level = next
	}
	return level[0]
}
