package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Layout is a person's saved arrangement of a page.
type Layout struct {
	Widgets   json.RawMessage `json:"widgets"`
	Version   int             `json:"version"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// UserLayout returns a person's layout of a page (ErrNotFound: none saved).
func (s *Store) UserLayout(ctx context.Context, userID, page string) (Layout, error) {
	var l Layout
	var w []byte
	err := s.DB.QueryRow(ctx, `SELECT widgets, version, updated_at FROM user_layouts WHERE user_id=$1 AND page=$2`, userID, page).Scan(&w, &l.Version, &l.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNotFound
	}
	l.Widgets = w
	return l, err
}

// SaveUserLayout stores a layout if it is still at version (0 = first save);
// otherwise ErrConflict (saved from another tab or device meanwhile).
func (s *Store) SaveUserLayout(ctx context.Context, userID, page string, widgets json.RawMessage, version int) (Layout, error) {
	var l Layout
	var w []byte
	var err error
	if version == 0 {
		err = s.DB.QueryRow(ctx, `INSERT INTO user_layouts (user_id, page, widgets) VALUES ($1,$2,$3)
			ON CONFLICT (user_id, page) DO NOTHING RETURNING widgets, version, updated_at`, userID, page, []byte(widgets)).Scan(&w, &l.Version, &l.UpdatedAt)
	} else {
		err = s.DB.QueryRow(ctx, `UPDATE user_layouts SET widgets=$3, version=version+1, updated_at=now()
			WHERE user_id=$1 AND page=$2 AND version=$4 RETURNING widgets, version, updated_at`, userID, page, []byte(widgets), version).Scan(&w, &l.Version, &l.UpdatedAt)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrConflict
	}
	l.Widgets = w
	return l, err
}

// ResetUserLayout forgets a person's layout.
func (s *Store) ResetUserLayout(ctx context.Context, userID, page string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM user_layouts WHERE user_id=$1 AND page=$2`, userID, page)
	return err
}
