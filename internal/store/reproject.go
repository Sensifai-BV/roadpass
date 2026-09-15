package store

import (
	"context"
	"time"

	"github.com/sensifai/roadpass/internal/domain"
)

// Reprojections returns up to limit queued ids, their records (latest versions)
// and the newest queue time read, for DoneReprojections.
func (s *Store) Reprojections(ctx context.Context, limit int) ([]string, []domain.Record, time.Time, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, queued_at FROM reproject ORDER BY queued_at LIMIT $1`, limit)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	var ids []string
	var upTo time.Time
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			rows.Close()
			return nil, nil, time.Time{}, err
		}
		ids = append(ids, id)
		if at.After(upTo) {
			upTo = at
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return nil, nil, upTo, err
	}
	recs, err := s.RecordsByID(ctx, ids)
	return ids, recs, upTo, err
}

// DoneReprojections removes projected ids from the queue, unless one was queued
// again after the read (it then stays for the next pass).
func (s *Store) DoneReprojections(ctx context.Context, ids []string, upTo time.Time) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM reproject WHERE id = ANY($1) AND queued_at <= $2`, ids, upTo)
	return err
}
