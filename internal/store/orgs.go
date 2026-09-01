package store

import (
	"context"
	"crypto/ed25519"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sensifai/roadpass/internal/domain"
)

const orgCols = `id, name, kind, kinds, COALESCE(tax_id,''), city, province, region, country, COALESCE(lat,0), COALESCE(lon,0), description,
	website, provenance, status, verified_at, created_at, legal_name, address, contact_email, phone, report_footer,
	EXISTS (SELECT 1 FROM org_logos l WHERE l.org_id = orgs.id), demo, public_ledger`

func scanOrg(row pgx.Row) (domain.Org, error) {
	var o domain.Org
	var kind string
	var kinds []string
	err := row.Scan(&o.ID, &o.Name, &kind, &kinds, &o.TaxID, &o.City, &o.Province, &o.Region, &o.Country, &o.Lat, &o.Lon,
		&o.Description, &o.Website, &o.Provenance, &o.Status, &o.VerifiedAt, &o.CreatedAt, &o.LegalName, &o.Address, &o.ContactEmail,
		&o.Phone, &o.ReportFooter, &o.HasLogo, &o.Demo, &o.PublicLedger)
	o.Kind = domain.OrgKind(kind)
	for _, k := range kinds {
		o.Kinds = append(o.Kinds, domain.OrgKind(k))
	}
	return o, err
}

// InsertOrg creates an organisation.
func (s *Store) InsertOrg(ctx context.Context, o domain.Org) error {
	if o.Status == "" {
		o.Status = domain.OrgPending
	}
	kinds := []string{string(o.Kind)}
	for _, k := range o.Kinds {
		if k != o.Kind && k.Valid() {
			kinds = append(kinds, string(k))
		}
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO orgs(id,name,kind,kinds,tax_id,city,province,region,country,lat,lon,description,website,provenance,status,verified_at,demo,public_ledger)
		VALUES ($1,$2,$3,$16,NULLIF($4,''),$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$17,$17 OR $18)`,
		o.ID, o.Name, string(o.Kind), o.TaxID, o.City, o.Province, o.Region, o.Country, o.Lat, o.Lon, o.Description, o.Website,
		o.Provenance, o.Status, o.VerifiedAt, kinds, o.Demo, o.PublicLedger)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// InsertSite creates a site. A site without a type takes its organisation's kind.
func (s *Store) InsertSite(ctx context.Context, x domain.Site) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO sites(id,org_id,name,city,province,region,lat,lon,capacity_t,equipment,kind,kind_other,partner,country,address)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,COALESCE(NULLIF($11,''),(SELECT kind FROM orgs WHERE id=$2)),$12,$13,
		COALESCE(NULLIF($14,''),(SELECT country FROM orgs WHERE id=$2)),$15)`,
		x.ID, x.OrgID, x.Name, x.City, x.Province, x.Region, nullZero(x.Lat, x.Lon), nullZero(x.Lon, x.Lat), x.CapacityT, x.Equipment,
		x.Kind, x.KindOther, x.Partner, x.Country, x.Address)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// UpdateSite changes one of an organisation's sites.
func (s *Store) UpdateSite(ctx context.Context, x domain.Site) error {
	tag, err := s.DB.Exec(ctx, `UPDATE sites SET name=$3, city=$4, province=$5, region=$6, lat=$7, lon=$8, capacity_t=$9, equipment=$10,
		kind=$11, kind_other=$12, partner=$13, country=$14, address=$15 WHERE id=$1 AND org_id=$2`,
		x.ID, x.OrgID, x.Name, x.City, x.Province, x.Region, nullZero(x.Lat, x.Lon), nullZero(x.Lon, x.Lat), x.CapacityT, x.Equipment,
		x.Kind, x.KindOther, x.Partner, x.Country, x.Address)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ArchiveSite hides a site from pickers (or brings it back). Records that name
// it keep doing so.
func (s *Store) ArchiveSite(ctx context.Context, orgID, id string, archived bool) error {
	tag, err := s.DB.Exec(ctx, `UPDATE sites SET archived_at = CASE WHEN $3 THEN COALESCE(archived_at, now()) END WHERE id=$1 AND org_id=$2`, id, orgID, archived)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// nullZero stores an unset location (0, 0) as NULL.
func nullZero(v, other float64) any {
	if v == 0 && other == 0 {
		return nil
	}
	return v
}

// LinkedOrgs lists the organisation itself and the organisations it already
// shares records with (as sender, receiver or road owner). Customers never see
// the rest of the platform.
func (s *Store) LinkedOrgs(ctx context.Context, orgID string) ([]string, error) {
	rows, err := s.DB.Query(ctx, `SELECT $1::text
		UNION SELECT counterparty FROM records WHERE org_id = $1 AND latest AND counterparty IS NOT NULL
		UNION SELECT org_id FROM records WHERE counterparty = $1 AND latest
		UNION SELECT body->>'owner_org_id' FROM records WHERE org_id = $1 AND latest AND type = 'road_section' AND body ? 'owner_org_id'
		UNION SELECT org_id FROM records WHERE latest AND type = 'road_section' AND body->>'owner_org_id' = $1`, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Org returns one organisation. Callers decide who may see it (LinkedOrgs).
func (s *Store) Org(ctx context.Context, id string) (domain.Org, error) {
	o, err := scanOrg(s.DB.QueryRow(ctx, `SELECT `+orgCols+` FROM orgs WHERE id=$1`, id))
	return o, notFound(err)
}

// Orgs lists every organisation, chain order then name.
func (s *Store) Orgs(ctx context.Context) ([]domain.Org, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+orgCols+` FROM orgs ORDER BY array_position(ARRAY['sorting_plant','recycler','additive_producer','asphalt_plant','contractor','road_owner','lab','auditor','operator'], kind), name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Org
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

const siteCols = `id, org_id, name, kind, kind_other, partner, address, city, province, region, country,
	COALESCE(lat,0), COALESCE(lon,0), capacity_t, equipment, archived_at`

func scanSite(row pgx.Row) (domain.Site, error) {
	var x domain.Site
	err := row.Scan(&x.ID, &x.OrgID, &x.Name, &x.Kind, &x.KindOther, &x.Partner, &x.Address, &x.City, &x.Province, &x.Region, &x.Country,
		&x.Lat, &x.Lon, &x.CapacityT, &x.Equipment, &x.ArchivedAt)
	return x, err
}

// Site returns one site.
func (s *Store) Site(ctx context.Context, id string) (domain.Site, error) {
	x, err := scanSite(s.DB.QueryRow(ctx, `SELECT `+siteCols+` FROM sites WHERE id = $1`, id))
	return x, notFound(err)
}

// Sites lists sites, optionally of one organisation.
func (s *Store) Sites(ctx context.Context, orgID string) ([]domain.Site, error) {
	return s.sites(ctx, `SELECT `+siteCols+` FROM sites WHERE $1 = '' OR org_id = $1 ORDER BY org_id, partner <> '', name`, orgID)
}

// VisibleSites lists every site of org (own and partner sites, archived ones
// too) and the own, active sites of the linked organisations. Partner sites are
// an organisation's private address book and never leave it.
func (s *Store) VisibleSites(ctx context.Context, org string, linked []string) ([]domain.Site, error) {
	return s.sites(ctx, `SELECT `+siteCols+` FROM sites WHERE org_id = $1
		OR (org_id = ANY($2) AND partner = '' AND archived_at IS NULL) ORDER BY org_id, partner <> '', name`, org, linked)
}

func (s *Store) sites(ctx context.Context, q string, args ...any) ([]domain.Site, error) {
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Site
	for rows.Next() {
		x, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SigningKey is a stored signing key: a company key (UserID empty) or a member's
// personal key. Custody says where the private half lives.
type SigningKey struct {
	ID           string
	OrgID        string
	UserID       string
	Public       ed25519.PublicKey
	Custody      string // platform | vault | customer
	SealedSeed   []byte // custody platform
	VaultKey     string // custody vault
	VaultVersion int
	Status       string // active | retired | revoked
	RevokeReason string
	LedgerTxID   string
	CreatedAt    time.Time
}

const sigKeyCols = `id, org_id, COALESCE(user_id,''), public_key, custody, sealed_seed, COALESCE(vault_key,''), COALESCE(vault_version,0),
	status, COALESCE(revoke_reason,''), COALESCE(ledger_tx_id,''), created_at`

func scanSigKey(row pgx.Row) (SigningKey, error) {
	var k SigningKey
	var pub []byte
	err := row.Scan(&k.ID, &k.OrgID, &k.UserID, &pub, &k.Custody, &k.SealedSeed, &k.VaultKey, &k.VaultVersion,
		&k.Status, &k.RevokeReason, &k.LedgerTxID, &k.CreatedAt)
	k.Public = pub
	return k, notFound(err)
}

// InsertSigningKey stores a key. A second active key for the same person is a
// conflict (two first actions racing to create it).
func (s *Store) InsertSigningKey(ctx context.Context, k SigningKey) error {
	if k.Custody == "" {
		k.Custody = "platform"
		if k.SealedSeed == nil {
			k.Custody = "customer"
		}
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO signing_keys(id,org_id,user_id,public_key,custody,sealed_seed,vault_key,vault_version,ledger_tx_id)
		VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,NULLIF($7,''),NULLIF($8,0),NULLIF($9,''))`,
		k.ID, k.OrgID, k.UserID, []byte(k.Public), k.Custody, k.SealedSeed, k.VaultKey, k.VaultVersion, k.LedgerTxID)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// SigningKeys returns every key (public halves are public; sealed seeds are only
// decrypted by the signer).
func (s *Store) SigningKeys(ctx context.Context) ([]SigningKey, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+sigKeyCols+` FROM signing_keys ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SigningKey
	for rows.Next() {
		k, err := scanSigKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// SigningKeysByID returns the named keys (missing ids are skipped).
func (s *Store) SigningKeysByID(ctx context.Context, ids []string) ([]SigningKey, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT `+sigKeyCols+` FROM signing_keys WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SigningKey
	for rows.Next() {
		k, err := scanSigKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// SigningKey returns one key.
func (s *Store) SigningKey(ctx context.Context, id string) (SigningKey, error) {
	return scanSigKey(s.DB.QueryRow(ctx, `SELECT `+sigKeyCols+` FROM signing_keys WHERE id=$1`, id))
}

// ActiveKey returns the active company key of an organisation (userID "") or a
// member's active personal key. Customer-held keys are skipped: the platform
// cannot sign with them.
func (s *Store) ActiveKey(ctx context.Context, orgID, userID string) (SigningKey, error) {
	if userID != "" {
		return scanSigKey(s.DB.QueryRow(ctx, `SELECT `+sigKeyCols+` FROM signing_keys WHERE user_id=$1 AND status='active'`, userID))
	}
	return scanSigKey(s.DB.QueryRow(ctx, `SELECT `+sigKeyCols+` FROM signing_keys WHERE org_id=$1 AND user_id IS NULL AND status='active'
		AND custody <> 'customer' ORDER BY created_at DESC LIMIT 1`, orgID))
}

// KeysOf lists a member's personal keys, newest first.
func (s *Store) KeysOf(ctx context.Context, userID string) ([]SigningKey, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+sigKeyCols+` FROM signing_keys WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SigningKey
	for rows.Next() {
		k, err := scanSigKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// CountKeys counts the keys ever issued to a holder (for the next key number).
func (s *Store) CountKeys(ctx context.Context, orgID, userID string) (int, error) {
	var n int
	err := s.DB.QueryRow(ctx, `SELECT count(*) FROM signing_keys WHERE CASE WHEN $2 = '' THEN org_id=$1 AND user_id IS NULL ELSE user_id=$2 END`,
		orgID, userID).Scan(&n)
	return n, err
}

// EndKey retires (rotation) or revokes (compromise, member left) an active key.
func (s *Store) EndKey(ctx context.Context, id, status, reason string) error {
	tag, err := s.DB.Exec(ctx, `UPDATE signing_keys SET status=$2, revoke_reason=NULLIF($3,''), revoked_at=now() WHERE id=$1 AND status='active'`,
		id, status, reason)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// MarkKeyRevokedOnLedger records the ledger transaction that revoked a key.
func (s *Store) MarkKeyRevokedOnLedger(ctx context.Context, id, txID string) error {
	_, err := s.DB.Exec(ctx, `UPDATE signing_keys SET ledger_revoke_tx_id=$2 WHERE id=$1`, id, txID)
	return err
}

// MarkKeyRegistered records the ledger transaction that registered a key.
func (s *Store) MarkKeyRegistered(ctx context.Context, id, txID string) error {
	_, err := s.DB.Exec(ctx, `UPDATE signing_keys SET ledger_tx_id=$2 WHERE id=$1`, id, txID)
	return err
}

// SetOrgStatus changes an organisation's verification status (operator action).
func (s *Store) SetOrgStatus(ctx context.Context, id, status, by string) error {
	tag, err := s.DB.Exec(ctx, `UPDATE orgs SET status=$2,
		verified_at = CASE WHEN $2='verified' THEN now() ELSE verified_at END,
		verified_by = CASE WHEN $2='verified' THEN $3 ELSE verified_by END WHERE id=$1`, id, status, by)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// UpdateOrgProfile lets an organisation admin edit its own profile.
func (s *Store) UpdateOrgProfile(ctx context.Context, o domain.Org) error {
	_, err := s.DB.Exec(ctx, `UPDATE orgs SET name=$2, tax_id=NULLIF($3,''), city=$4, province=$5, region=$6, lat=$7, lon=$8,
		description=$9, website=$10, country=$11, legal_name=$12, address=$13, contact_email=$14, phone=$15, report_footer=$16 WHERE id=$1`,
		o.ID, o.Name, o.TaxID, o.City, o.Province, o.Region, o.Lat, o.Lon, o.Description, o.Website, o.Country,
		o.LegalName, o.Address, o.ContactEmail, o.Phone, o.ReportFooter)
	return err
}

// MarkOrgSimulated makes an organisation a test organisation: the records it
// files from now on are labelled Simulated (ingest refuses Operational ones).
func (s *Store) MarkOrgSimulated(ctx context.Context, orgID string) error {
	_, err := s.DB.Exec(ctx, `UPDATE orgs SET provenance=$2 WHERE id=$1`, orgID, domain.ProvSimulated)
	return err
}

// OrgLogo returns an organisation's logo.
func (s *Store) OrgLogo(ctx context.Context, orgID string) (contentType string, data []byte, at time.Time, err error) {
	err = s.DB.QueryRow(ctx, `SELECT content_type, data, updated_at FROM org_logos WHERE org_id=$1`, orgID).Scan(&contentType, &data, &at)
	return contentType, data, at, notFound(err)
}

// SetOrgLogo stores (or with nil data removes) an organisation's logo.
func (s *Store) SetOrgLogo(ctx context.Context, orgID, contentType string, data []byte) error {
	if data == nil {
		_, err := s.DB.Exec(ctx, `DELETE FROM org_logos WHERE org_id=$1`, orgID)
		return err
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO org_logos(org_id, content_type, data) VALUES ($1,$2,$3)
		ON CONFLICT (org_id) DO UPDATE SET content_type=EXCLUDED.content_type, data=EXCLUDED.data, updated_at=now()`, orgID, contentType, data)
	return err
}
