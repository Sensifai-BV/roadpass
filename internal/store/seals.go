package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sensifai/roadpass/internal/proof"
)

// Seal is one Merkle root anchored (or being anchored) on the ledger.
type Seal struct {
	ID          string     `json:"id"`
	OrgID       string     `json:"orgId,omitempty"` // "" = a shared seal made before seals were dedicated
	MerkleRoot  string     `json:"merkleRoot"`
	LeafCount   int        `json:"leafCount"`
	FirstSeq    int64      `json:"firstSeq"`
	LastSeq     int64      `json:"lastSeq"`
	Provenance  string     `json:"provenance"`
	Status      string     `json:"status"`
	TxID        string     `json:"txId,omitempty"`
	BlockNumber int64      `json:"blockNumber,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	SealedAt    *time.Time `json:"sealedAt,omitempty"`
}

// Leaf is one record hash in seal order.
type Leaf struct {
	Seq  int64
	ID   string
	Ver  int
	Hash string
	Prov string
}

// ClaimLeaves starts a pending seal over up to max unsealed, non-blocked records of
// one organisation in seq order and assigns their leaf indexes, all in one
// transaction. The organisation is the one owning the oldest unsealed record, so
// organisations are served first come, first served. Each seal is dedicated to one
// organisation, and with one seal transaction per block, so is each block. newID
// names the seal for that organisation. SKIP LOCKED lets several sealers run
// without sealing a record twice.
func (s *Store) ClaimLeaves(ctx context.Context, newID func(orgID string) string, max int, root func([]string) (string, error)) (*Seal, []Leaf, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	// Only verified organisations' records are sealed: the ledger must not vouch for
	// a company the operator has not checked (docs/ARCHITECTURE.md §4a).
	var org string
	err = tx.QueryRow(ctx, `SELECT r.org_id FROM records r
		WHERE r.seal_id IS NULL AND r.status <> 'blocked'
		  AND r.org_id IN (SELECT id FROM orgs WHERE status = 'verified')
		ORDER BY r.seq LIMIT 1 FOR UPDATE OF r SKIP LOCKED`).Scan(&org)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	rows, err := tx.Query(ctx, `SELECT r.seq, r.id, r.version, r.content_hash, r.provenance FROM records r
		WHERE r.seal_id IS NULL AND r.status <> 'blocked' AND r.org_id = $2
		ORDER BY r.seq LIMIT $1 FOR UPDATE OF r SKIP LOCKED`, max, org)
	if err != nil {
		return nil, nil, err
	}
	var leaves []Leaf
	for rows.Next() {
		var l Leaf
		if err := rows.Scan(&l.Seq, &l.ID, &l.Ver, &l.Hash, &l.Prov); err != nil {
			rows.Close()
			return nil, nil, err
		}
		leaves = append(leaves, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(leaves) == 0 {
		return nil, nil, err
	}
	hashes := make([]string, len(leaves))
	prov := leaves[0].Prov
	for i, l := range leaves {
		hashes[i] = l.Hash
		if l.Prov != prov {
			prov = "Mixed"
		}
	}
	r, err := root(hashes)
	if err != nil {
		return nil, nil, err
	}
	sealID := newID(org)
	seal := &Seal{ID: sealID, OrgID: org, MerkleRoot: r, LeafCount: len(leaves), FirstSeq: leaves[0].Seq, LastSeq: leaves[len(leaves)-1].Seq,
		Provenance: prov, Status: "pending", CreatedAt: time.Now().UTC()}
	if _, err := tx.Exec(ctx, `INSERT INTO seals(id,org_id,merkle_root,leaf_count,first_seq,last_seq,provenance,status) VALUES ($1,$2,$3,$4,$5,$6,$7,'pending')`,
		seal.ID, seal.OrgID, seal.MerkleRoot, seal.LeafCount, seal.FirstSeq, seal.LastSeq, seal.Provenance); err != nil {
		return nil, nil, err
	}
	seqs := make([]int64, len(leaves))
	idx := make([]int32, len(leaves))
	for i, l := range leaves {
		seqs[i], idx[i] = l.Seq, int32(i)
	}
	if _, err := tx.Exec(ctx, `UPDATE records r SET seal_id=$1, leaf_index=u.idx
		FROM unnest($2::bigint[], $3::int[]) AS u(seq, idx) WHERE r.seq=u.seq`, sealID, seqs, idx); err != nil {
		return nil, nil, err
	}
	return seal, leaves, tx.Commit(ctx)
}

// MarkSealed records a successful ledger transaction.
func (s *Store) MarkSealed(ctx context.Context, id, txID string, block int64, at time.Time) error {
	_, err := s.DB.Exec(ctx, `UPDATE seals SET status='sealed', tx_id=$2, block_number=NULLIF($3,0), sealed_at=$4, error=NULL WHERE id=$1`, id, txID, block, at)
	return err
}

// MarkSealFailed records a failure; the seal is retried with the same leaves and
// the same id (the chaincode refuses a second seal with that id, so a retry after
// an unseen success is detected as "already exists" and resolved by GetSeal).
func (s *Store) MarkSealFailed(ctx context.Context, id, msg string) error {
	_, err := s.DB.Exec(ctx, `UPDATE seals SET status='failed', error=$2 WHERE id=$1`, id, msg)
	return err
}

// PendingSeals returns seals that still need a ledger transaction.
func (s *Store) PendingSeals(ctx context.Context, limit int) ([]Seal, error) {
	return s.seals(ctx, `WHERE status IN ('pending','failed') ORDER BY created_at LIMIT $1`, limit)
}

// Seals lists the newest seals.
func (s *Store) Seals(ctx context.Context, limit int) ([]Seal, error) {
	return s.seals(ctx, `ORDER BY created_at DESC LIMIT $1`, limit)
}

// GetSeal returns one seal.
func (s *Store) GetSeal(ctx context.Context, id string) (Seal, error) {
	out, err := s.seals(ctx, `WHERE id=$1`, id)
	if err != nil {
		return Seal{}, err
	}
	if len(out) == 0 {
		return Seal{}, ErrNotFound
	}
	return out[0], nil
}

func (s *Store) seals(ctx context.Context, tail string, args ...any) ([]Seal, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, COALESCE(org_id,''), merkle_root, leaf_count, first_seq, last_seq, provenance, status,
		COALESCE(tx_id,''), COALESCE(block_number,0), COALESCE(error,''), created_at, sealed_at FROM seals `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Seal
	for rows.Next() {
		var x Seal
		if err := rows.Scan(&x.ID, &x.OrgID, &x.MerkleRoot, &x.LeafCount, &x.FirstSeq, &x.LastSeq, &x.Provenance, &x.Status,
			&x.TxID, &x.BlockNumber, &x.Error, &x.CreatedAt, &x.SealedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SealLeaves returns the hashes of a seal in leaf order.
func (s *Store) SealLeaves(ctx context.Context, sealID string) ([]string, error) {
	rows, err := s.DB.Query(ctx, `SELECT content_hash FROM records WHERE seal_id=$1 ORDER BY leaf_index`, sealID)
	if err != nil {
		return nil, err
	}
	hs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	return hs, nil
}

// SealStats summarises sealing progress.
type SealStats struct {
	Records  int64 `json:"records"`
	Sealed   int64 `json:"sealed"`
	Pending  int64 `json:"pending"`
	Blocked  int64 `json:"blocked"`
	Seals    int64 `json:"seals"`
	LastSeal *Seal `json:"lastSeal,omitempty"`
	// Records counts every version (a correction adds one; each is sealed on its
	// own). Current and CurrentSealed count only the latest version of each record.
	Current       int64 `json:"current"`
	CurrentSealed int64 `json:"currentSealed"`
}

// SealStatistics counts sealed and unsealed record versions.
func (s *Store) SealStatistics(ctx context.Context) (SealStats, error) {
	var st SealStats
	err := s.DB.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM records),
			(SELECT COALESCE(sum(leaf_count),0) FROM seals WHERE status='sealed'),
			(SELECT count(*) FROM records WHERE status='blocked'),
			(SELECT count(*) FROM seals WHERE status='sealed'),
			(SELECT count(*) FROM records WHERE latest),
			(SELECT count(*) FROM records r JOIN seals s ON s.id = r.seal_id AND s.status='sealed' WHERE r.latest)`).Scan(
		&st.Records, &st.Sealed, &st.Blocked, &st.Seals, &st.Current, &st.CurrentSealed)
	if err != nil {
		return st, err
	}
	st.Pending = st.Records - st.Sealed - st.Blocked
	if last, err := s.seals(ctx, `WHERE status='sealed' ORDER BY sealed_at DESC LIMIT 1`); err == nil && len(last) == 1 {
		st.LastSeal = &last[0]
	}
	return st, nil
}

// NextSealID returns a unique seal id naming the organisation the seal is
// dedicated to, so the block's transaction says on the ledger whose it is.
func NextSealID(orgID string, now time.Time, n int64) string {
	return fmt.Sprintf("SEAL-%s-%s-%06d", orgID, now.UTC().Format("20060102T150405"), n)
}

// SealSummary is a seal with the viewer's share of it.
type SealSummary struct {
	Seal
	Mine int `json:"mine"`
}

// SealsFor lists the newest seals containing an organisation's records ("" = all).
func (s *Store) SealsFor(ctx context.Context, orgID string, limit int) ([]SealSummary, error) {
	q := `SELECT s.id, COALESCE(s.org_id,''), s.merkle_root, s.leaf_count, s.first_seq, s.last_seq, s.provenance, s.status, COALESCE(s.tx_id,''),
			COALESCE(s.block_number,0), COALESCE(s.error,''), s.created_at, s.sealed_at, s.leaf_count
		FROM seals s ORDER BY s.created_at DESC LIMIT $1`
	args := []any{limit}
	if orgID != "" {
		q = `SELECT s.id, COALESCE(s.org_id,''), s.merkle_root, s.leaf_count, s.first_seq, s.last_seq, s.provenance, s.status, COALESCE(s.tx_id,''),
				COALESCE(s.block_number,0), COALESCE(s.error,''), s.created_at, s.sealed_at, m.n
			FROM (SELECT seal_id, count(*) AS n FROM records WHERE org_id=$2 AND seal_id IS NOT NULL GROUP BY seal_id) m
			JOIN seals s ON s.id = m.seal_id ORDER BY s.created_at DESC LIMIT $1`
		args = append(args, orgID)
	}
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SealSummary
	for rows.Next() {
		var x SealSummary
		if err := rows.Scan(&x.ID, &x.OrgID, &x.MerkleRoot, &x.LeafCount, &x.FirstSeq, &x.LastSeq, &x.Provenance, &x.Status,
			&x.TxID, &x.BlockNumber, &x.Error, &x.CreatedAt, &x.SealedAt, &x.Mine); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SealMember is one record in a seal.
type SealMember struct {
	ID        string `json:"id"`
	Version   int    `json:"version"`
	Type      string `json:"type"`
	OrgID     string `json:"orgId"`
	LeafIndex int    `json:"leafIndex"`
	Hash      string `json:"contentHash"`
}

// SealMembers lists the viewer's records in a seal and counts them.
func (s *Store) SealMembers(ctx context.Context, sealID string, v Viewer, limit int) ([]SealMember, int, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, version, type, org_id, leaf_index, content_hash FROM records
		WHERE seal_id=$1 AND ($2 OR org_id=$3 OR counterparty=$3) ORDER BY leaf_index`, sealID, v.All(), v.OrgID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SealMember
	n := 0
	for rows.Next() {
		var m SealMember
		if err := rows.Scan(&m.ID, &m.Version, &m.Type, &m.OrgID, &m.LeafIndex, &m.Hash); err != nil {
			return nil, 0, err
		}
		n++
		if len(out) < limit {
			out = append(out, m)
		}
	}
	return out, n, rows.Err()
}

// OrgSealIDs returns the ids of seals that contain an organisation's records.
func (s *Store) OrgSealIDs(ctx context.Context, orgID string) (map[string]bool, error) {
	rows, err := s.DB.Query(ctx, `SELECT DISTINCT seal_id FROM records WHERE org_id=$1 AND seal_id IS NOT NULL`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// OrgSealStats counts an organisation's sealed and waiting record versions.
func (s *Store) OrgSealStats(ctx context.Context, orgID string) (SealStats, error) {
	var st SealStats
	err := s.DB.QueryRow(ctx, `SELECT count(*),
			count(*) FILTER (WHERE r.seal_id IS NOT NULL AND s.status='sealed'),
			count(*) FILTER (WHERE r.status='blocked'),
			count(DISTINCT r.seal_id),
			count(*) FILTER (WHERE r.latest),
			count(*) FILTER (WHERE r.latest AND r.seal_id IS NOT NULL AND s.status='sealed')
		FROM records r LEFT JOIN seals s ON s.id = r.seal_id WHERE r.org_id=$1`, orgID).Scan(
		&st.Records, &st.Sealed, &st.Blocked, &st.Seals, &st.Current, &st.CurrentSealed)
	if err != nil {
		return st, err
	}
	st.Pending = st.Records - st.Sealed - st.Blocked
	// The organisation's own last seal, not the platform's.
	if last, err := s.seals(ctx, `WHERE status='sealed' AND id IN (SELECT seal_id FROM records WHERE org_id=$1 AND seal_id IS NOT NULL)
		ORDER BY sealed_at DESC LIMIT 1`, orgID); err == nil && len(last) == 1 {
		st.LastSeal = &last[0]
	}
	return st, nil
}

// Verification is the answer to "is this fingerprint on the ledger?".
type Verification struct {
	Hash         string       `json:"hash"`
	Found        bool         `json:"found"`
	RecordID     string       `json:"recordId,omitempty"`
	Version      int          `json:"version,omitempty"`
	Type         string       `json:"type,omitempty"`
	OrgName      string       `json:"orgName,omitempty"`
	OccurredAt   *time.Time   `json:"occurredAt,omitempty"`
	Latest       bool         `json:"latest"`
	SealID       string       `json:"sealId,omitempty"`
	SealStatus   string       `json:"sealStatus,omitempty"`
	MerkleRoot   string       `json:"merkleRoot,omitempty"`
	TxID         string       `json:"txId,omitempty"`
	BlockNumber  int64        `json:"blockNumber,omitempty"`
	SealedAt     *time.Time   `json:"sealedAt,omitempty"`
	LeafIndex    int          `json:"leafIndex"`
	Path         []proof.Step `json:"path,omitempty"`
	ComputedRoot string       `json:"computedRoot,omitempty"`
	Included     bool         `json:"included"`
	LedgerRoot   string       `json:"ledgerRoot,omitempty"`
	Anchored     bool         `json:"anchored"`
	LedgerError  string       `json:"ledgerError,omitempty"`
}

// VerifyHash looks a content hash up.
func (s *Store) VerifyHash(ctx context.Context, hash string) (Verification, error) {
	v := Verification{Hash: hash}
	var at time.Time
	err := s.DB.QueryRow(ctx, `SELECT r.id, r.version, r.type, o.name, r.occurred_at, r.latest, COALESCE(r.seal_id,''),
			COALESCE(r.leaf_index,0), COALESCE(s.status,''), COALESCE(s.merkle_root,''), COALESCE(s.tx_id,''),
			COALESCE(s.block_number,0), s.sealed_at
		FROM records r JOIN orgs o ON o.id = r.org_id LEFT JOIN seals s ON s.id = r.seal_id
		WHERE r.content_hash=$1 ORDER BY r.latest DESC LIMIT 1`, hash).
		Scan(&v.RecordID, &v.Version, &v.Type, &v.OrgName, &at, &v.Latest, &v.SealID, &v.LeafIndex, &v.SealStatus,
			&v.MerkleRoot, &v.TxID, &v.BlockNumber, &v.SealedAt)
	if err == pgx.ErrNoRows {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	v.Found, v.OccurredAt = true, &at
	return v, nil
}

// OrgBlocks is the part of the ledger dedicated to one organisation: its seals and
// the blocks that hold their transactions. OrgID "" collects the shared seals made
// before seals were dedicated.
type OrgBlocks struct {
	OrgID     string     `json:"orgId"`
	Seals     int        `json:"seals"`
	Records   int64      `json:"records"`
	Blocks    []int64    `json:"blocks"`
	Pending   int        `json:"pendingSeals"`
	LastSeal  *time.Time `json:"lastSealedAt,omitempty"`
	FirstSeal *time.Time `json:"firstSealedAt,omitempty"`
}

// LedgerByOrg groups seals by the organisation they are dedicated to ("" = all).
func (s *Store) LedgerByOrg(ctx context.Context, orgID string) ([]OrgBlocks, error) {
	rows, err := s.DB.Query(ctx, `SELECT COALESCE(org_id,''),
			count(*) FILTER (WHERE status='sealed'),
			COALESCE(sum(leaf_count) FILTER (WHERE status='sealed'),0),
			COALESCE(array_agg(block_number ORDER BY block_number) FILTER (WHERE status='sealed' AND block_number IS NOT NULL), '{}'),
			count(*) FILTER (WHERE status<>'sealed'),
			max(sealed_at), min(sealed_at)
		FROM seals WHERE ($1 = '' OR org_id = $1)
		GROUP BY 1 ORDER BY count(*) FILTER (WHERE status='sealed') DESC, 1`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrgBlocks
	for rows.Next() {
		var x OrgBlocks
		if err := rows.Scan(&x.OrgID, &x.Seals, &x.Records, &x.Blocks, &x.Pending, &x.LastSeal, &x.FirstSeal); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// DedicatedSeals lists the newest seals dedicated to one organisation ("" with
// shared=true: the shared seals made before seals were dedicated).
func (s *Store) DedicatedSeals(ctx context.Context, orgID string, limit int) ([]Seal, error) {
	if orgID == "" {
		return s.seals(ctx, `WHERE org_id IS NULL ORDER BY created_at DESC LIMIT $1`, limit)
	}
	return s.seals(ctx, `WHERE org_id=$2 ORDER BY created_at DESC LIMIT $1`, limit, orgID)
}
