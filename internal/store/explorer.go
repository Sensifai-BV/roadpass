package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The public explorer shows only organisations whose admins turned the public
// ledger on (orgs.public_ledger). Everything below is limited to their seals.
const publicOrg = `(SELECT id FROM orgs WHERE public_ledger AND status = 'verified')`

// SetPublicLedger turns an organisation's public explorer listing on or off.
func (s *Store) SetPublicLedger(ctx context.Context, orgID string, on bool) error {
	_, err := s.DB.Exec(ctx, `UPDATE orgs SET public_ledger=$2 WHERE id=$1`, orgID, on)
	return err
}

// PublicOrg is an organisation listed on the explorer.
type PublicOrg struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Kind     string     `json:"kind"`
	Country  string     `json:"country"`
	Demo     bool       `json:"demo,omitempty"`
	HasLogo  bool       `json:"hasLogo,omitempty"`
	Seals    int        `json:"seals"`
	Records  int64      `json:"records"`
	Blocks   []int64    `json:"blocks"`
	LastSeal *time.Time `json:"lastSealedAt,omitempty"`
}

// PublicOrgs lists the public organisations with their sealed totals.
func (s *Store) PublicOrgs(ctx context.Context) ([]PublicOrg, error) {
	rows, err := s.DB.Query(ctx, `SELECT o.id, o.name, o.kind, o.country, o.demo,
			EXISTS (SELECT 1 FROM org_logos l WHERE l.org_id = o.id),
			count(s.id), COALESCE(sum(s.leaf_count),0),
			COALESCE(array_agg(s.block_number ORDER BY s.block_number DESC) FILTER (WHERE s.block_number IS NOT NULL), '{}'),
			max(s.sealed_at)
		FROM orgs o LEFT JOIN seals s ON s.org_id = o.id AND s.status = 'sealed'
		WHERE o.public_ledger AND o.status = 'verified'
		GROUP BY o.id ORDER BY max(s.sealed_at) DESC NULLS LAST, o.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PublicOrg{}
	for rows.Next() {
		var x PublicOrg
		if err := rows.Scan(&x.ID, &x.Name, &x.Kind, &x.Country, &x.Demo, &x.HasLogo, &x.Seals, &x.Records, &x.Blocks, &x.LastSeal); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// IsPublicOrg reports whether an organisation is listed on the explorer.
func (s *Store) IsPublicOrg(ctx context.Context, id string) bool {
	var ok bool
	_ = s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM orgs WHERE id=$1 AND public_ledger AND status='verified')`, id).Scan(&ok)
	return ok
}

// PublicSeal returns a sealed seal of a public organisation.
func (s *Store) PublicSeal(ctx context.Context, id string) (Seal, error) {
	out, err := s.seals(ctx, `WHERE id=$1 AND status='sealed' AND org_id IN `+publicOrg, id)
	if err != nil {
		return Seal{}, err
	}
	if len(out) == 0 {
		return Seal{}, ErrNotFound
	}
	return out[0], nil
}

// ChainSeal is a seal as the explorer shows it in a block: Public says whether
// its organisation published its ledger data. A private seal is shown by its
// hashes only; the caller must not expose anything else about it.
type ChainSeal struct {
	Seal
	Public bool `json:"-"`
}

// ChainSeals returns the sealed seals that sit in the given blocks.
func (s *Store) ChainSeals(ctx context.Context, blocks []int64) ([]ChainSeal, error) {
	if len(blocks) == 0 {
		return nil, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT s.id, COALESCE(s.org_id,''), s.merkle_root, s.leaf_count, s.first_seq, s.last_seq, s.provenance, s.status,
			COALESCE(s.tx_id,''), COALESCE(s.block_number,0), COALESCE(s.error,''), s.created_at, s.sealed_at,
			COALESCE(o.public_ledger AND o.status='verified', false)
		FROM seals s LEFT JOIN orgs o ON o.id = s.org_id
		WHERE s.status='sealed' AND s.block_number = ANY($1) ORDER BY s.created_at`, blocks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChainSeal
	for rows.Next() {
		var x ChainSeal
		if err := rows.Scan(&x.ID, &x.OrgID, &x.MerkleRoot, &x.LeafCount, &x.FirstSeq, &x.LastSeq, &x.Provenance, &x.Status,
			&x.TxID, &x.BlockNumber, &x.Error, &x.CreatedAt, &x.SealedAt, &x.Public); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// PublicKeyOrgs maps signing key ids to their organisation, for public organisations only.
func (s *Store) PublicKeyOrgs(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT id, org_id FROM signing_keys WHERE id = ANY($1) AND org_id IN `+publicOrg, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, org string
		if err := rows.Scan(&id, &org); err != nil {
			return nil, err
		}
		out[id] = org
	}
	return out, rows.Err()
}

// PublicSealsOf lists the newest seals of one public organisation.
func (s *Store) PublicSealsOf(ctx context.Context, orgID string, limit int) ([]Seal, error) {
	return s.seals(ctx, `WHERE org_id=$2 AND status='sealed' AND org_id IN `+publicOrg+` ORDER BY created_at DESC LIMIT $1`, limit, orgID)
}

// PublicRecord is one record in a public organisation's seal: its supply-chain facts.
type PublicRecord struct {
	Index      int       `json:"leafIndex"`
	ID         string    `json:"id"`
	Version    int       `json:"version"`
	Type       string    `json:"type"`
	Ref        string    `json:"ref"`
	OccurredAt time.Time `json:"occurredAt"`
	MassKg     float64   `json:"massKg"`
	Provenance string    `json:"provenance"`
	Hash       string    `json:"contentHash"`
}

// PublicRecords lists the records of a seal in leaf order.
func (s *Store) PublicRecords(ctx context.Context, sealID string, limit int) ([]PublicRecord, error) {
	rows, err := s.DB.Query(ctx, `SELECT leaf_index, id, version, type, ref, occurred_at, mass_kg, provenance, content_hash
		FROM records WHERE seal_id=$1 ORDER BY leaf_index LIMIT $2`, sealID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PublicRecord{}
	for rows.Next() {
		var x PublicRecord
		if err := rows.Scan(&x.Index, &x.ID, &x.Version, &x.Type, &x.Ref, &x.OccurredAt, &x.MassKg, &x.Provenance, &x.Hash); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// TypeTotal counts one record type of an organisation.
type TypeTotal struct {
	Type   string  `json:"type"`
	Count  int     `json:"count"`
	MassKg float64 `json:"massKg"`
	Sealed int     `json:"sealed"`
}

// PublicSite is one of an organisation's own sites.
type PublicSite struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	City    string `json:"city"`
	Region  string `json:"region"`
	Country string `json:"country"`
}

// PublicOrgDetail is the supply-chain profile of a public organisation.
type PublicOrgDetail struct {
	Org   PublicOrg      `json:"org"`
	Kinds []string       `json:"kinds"`
	City  string         `json:"city"`
	Site  string         `json:"website,omitempty"`
	About string         `json:"description,omitempty"`
	Types []TypeTotal    `json:"types"`
	Sites []PublicSite   `json:"sites"`
	Roads []PublicRecord `json:"roadSections"`
}

// PublicOrgDetail returns an organisation's profile, record totals by type, own sites and road sections.
func (s *Store) PublicOrgDetail(ctx context.Context, id string) (PublicOrgDetail, error) {
	var d PublicOrgDetail
	list, err := s.PublicOrgs(ctx)
	if err != nil {
		return d, err
	}
	found := false
	for _, o := range list {
		if o.ID == id {
			d.Org, found = o, true
		}
	}
	if !found {
		return d, ErrNotFound
	}
	if err := s.DB.QueryRow(ctx, `SELECT kinds, city, website, description FROM orgs WHERE id=$1`, id).Scan(&d.Kinds, &d.City, &d.Site, &d.About); err != nil {
		return d, err
	}
	rows, err := s.DB.Query(ctx, `SELECT type, count(*), COALESCE(sum(mass_kg),0), count(*) FILTER (WHERE seal_id IS NOT NULL)
		FROM records WHERE org_id=$1 AND latest GROUP BY type ORDER BY count(*) DESC`, id)
	if err != nil {
		return d, err
	}
	d.Types = []TypeTotal{}
	for rows.Next() {
		var t TypeTotal
		if err := rows.Scan(&t.Type, &t.Count, &t.MassKg, &t.Sealed); err != nil {
			rows.Close()
			return d, err
		}
		d.Types = append(d.Types, t)
	}
	rows.Close()
	srows, err := s.DB.Query(ctx, `SELECT name, kind, city, region, COALESCE(country,'') FROM sites WHERE org_id=$1 AND COALESCE(partner,'')='' AND archived_at IS NULL ORDER BY name LIMIT 100`, id)
	if err != nil {
		return d, err
	}
	d.Sites = []PublicSite{}
	for srows.Next() {
		var x PublicSite
		if err := srows.Scan(&x.Name, &x.Kind, &x.City, &x.Region, &x.Country); err != nil {
			srows.Close()
			return d, err
		}
		d.Sites = append(d.Sites, x)
	}
	srows.Close()
	rrows, err := s.DB.Query(ctx, `SELECT COALESCE(leaf_index,0), id, version, type, ref, occurred_at, mass_kg, provenance, content_hash
		FROM records WHERE org_id=$1 AND latest AND type='road_section' ORDER BY occurred_at DESC LIMIT 50`, id)
	if err != nil {
		return d, err
	}
	defer rrows.Close()
	d.Roads = []PublicRecord{}
	for rrows.Next() {
		var x PublicRecord
		if err := rrows.Scan(&x.Index, &x.ID, &x.Version, &x.Type, &x.Ref, &x.OccurredAt, &x.MassKg, &x.Provenance, &x.Hash); err != nil {
			return d, err
		}
		d.Roads = append(d.Roads, x)
	}
	return d, rrows.Err()
}

// ExplorerTotals counts the whole chain: every seal, public or not (numbers only).
type ExplorerTotals struct {
	Orgs    int   `json:"orgs"` // organisations publishing their ledger data
	Seals   int   `json:"seals"`
	Records int64 `json:"records"`
	Blocks  int   `json:"blocks"`
}

func (s *Store) ExplorerTotals(ctx context.Context) (ExplorerTotals, error) {
	var t ExplorerTotals
	err := s.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM orgs WHERE public_ledger AND status='verified'),
			count(*), COALESCE(sum(leaf_count),0), count(DISTINCT block_number)
		FROM seals WHERE status='sealed'`).Scan(&t.Orgs, &t.Seals, &t.Records, &t.Blocks)
	return t, err
}

// ExplorerSearch resolves a seal id, transaction id or Merkle root. A public seal
// opens its seal page; a private one only its block.
func (s *Store) ExplorerSearch(ctx context.Context, q string) (kind, id string, err error) {
	var sealID string
	var block int64
	var public bool
	err = s.DB.QueryRow(ctx, `SELECT s.id, COALESCE(s.block_number,0), COALESCE(o.public_ledger AND o.status='verified', false)
		FROM seals s LEFT JOIN orgs o ON o.id = s.org_id
		WHERE s.status='sealed' AND (s.id=$1 OR s.tx_id=$2 OR s.merkle_root=$2) LIMIT 1`, q, strings.ToLower(q)).Scan(&sealID, &block, &public)
	if isNoRows(err) {
		// a record fingerprint inside a public seal
		err = s.DB.QueryRow(ctx, `SELECT id FROM seals WHERE status='sealed' AND org_id IN `+publicOrg+`
			AND id IN (SELECT seal_id FROM records WHERE content_hash=$1 AND seal_id IS NOT NULL) LIMIT 1`, strings.ToLower(q)).Scan(&sealID)
		if isNoRows(err) {
			return "", "", nil
		}
		return "seal", sealID, err
	}
	if err != nil {
		return "", "", err
	}
	if public {
		return "seal", sealID, nil
	}
	if block == 0 {
		return "", "", nil
	}
	return "block", strconv.FormatInt(block, 10), nil
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
