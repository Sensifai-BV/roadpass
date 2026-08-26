package proof

import (
	"fmt"
	"testing"
)

func TestInclusionProofEveryLeafEverySize(t *testing.T) {
	for n := 1; n <= 33; n++ {
		leaves := make([]string, n)
		for i := range leaves {
			leaves[i] = HashBytes([]byte(fmt.Sprintf("r%d", i)))
		}
		want, err := MerkleRoot(leaves)
		if err != nil {
			t.Fatal(err)
		}
		for i := range leaves {
			root, path, err := InclusionProof(leaves, i)
			if err != nil {
				t.Fatal(err)
			}
			if root != want {
				t.Fatalf("n=%d i=%d: proof root %s != root %s", n, i, root, want)
			}
			if !VerifyInclusion(leaves[i], path, root) {
				t.Fatalf("n=%d i=%d: valid proof rejected", n, i)
			}
			other := HashBytes([]byte("tampered"))
			if VerifyInclusion(other, path, root) {
				t.Fatalf("n=%d i=%d: tampered leaf accepted", n, i)
			}
		}
	}
}

func TestInclusionProofRejectsBadInput(t *testing.T) {
