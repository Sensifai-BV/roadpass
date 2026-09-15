package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/sensifai/roadpass/internal/auth"
)

// ShareToken returns the passport's active public-link token, or ErrNotFound
// when it is not shared.
func (s *Store) ShareToken(ctx context.Context, recordID string) (string, error) {
	var tok string
	err := s.DB.QueryRow(ctx, `SELECT token FROM passport_shares WHERE record_id=$1 AND revoked_at IS NULL`, recordID).Scan(&tok)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return tok, err
}

// EnsureShare returns the active token for a passport, creating one when none exists.
func (s *Store) EnsureShare(ctx context.Context, recordID, orgID, by string) (string, error) {
	if tok, err := s.ShareToken(ctx, recordID); err == nil {
		return tok, nil
	} else if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	tok := auth.Token("", 18) // 144 bits, 24 URL-safe characters
	_, err := s.DB.Exec(ctx, `INSERT INTO passport_shares(token, record_id, org_id, created_by) VALUES ($1,$2,$3,$4)
		ON CONFLICT DO NOTHING`, tok, recordID, orgID, by)
	if err != nil {
		return "", err
	}
	// A concurrent request may have won the unique index: return whichever is active.
	return s.ShareToken(ctx, recordID)
}

// RotateShare revokes the active token and issues a new one.
func (s *Store) RotateShare(ctx context.Context, recordID, orgID, by string) (string, error) {
	if err := s.RevokeShare(ctx, recordID); err != nil {
		return "", err
	}
	return s.EnsureShare(ctx, recordID, orgID, by)
}

// RevokeShare stops sharing: the passport's links no longer resolve.
func (s *Store) RevokeShare(ctx context.Context, recordID string) error {
	_, err := s.DB.Exec(ctx, `UPDATE passport_shares SET revoked_at=now() WHERE record_id=$1 AND revoked_at IS NULL`, recordID)
	return err
}

// ResolveShare maps a public token to its passport id; ErrNotFound when the
// token is unknown or revoked.
func (s *Store) ResolveShare(ctx context.Context, token string) (string, error) {
	var id string
	err := s.DB.QueryRow(ctx, `SELECT record_id FROM passport_shares WHERE token=$1 AND revoked_at IS NULL`, token).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}
