package domain

import "github.com/sensifai/roadpass/internal/proof"

// ComputeHash returns the content hash of r: SHA-256 over the RFC 8785 canonical
// JSON of r.HashDoc().
func (r *Record) ComputeHash() (string, error) { return proof.HashJSON(r.HashDoc()) }

// DupHash fingerprints the business content only (no id, reference or version), so
// the same weighing submitted twice under two references is recognised as a
// duplicate even though the two content hashes differ.
func (r *Record) DupHash() (string, error) {
	d := r.HashDoc()
	d.ID, d.Ref, d.Version, d.PrevHash, d.Note = "", "", 0, "", ""
	return proof.HashJSON(d)
}
