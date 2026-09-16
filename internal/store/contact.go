package store

import (
	"context"
	"time"
)

// ContactMessage is one message from the public contact form.
type ContactMessage struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Email     string     `json:"email"`
	Company   string     `json:"company,omitempty"`
	Topic     string     `json:"topic"`
	Message   string     `json:"message"`
	CreatedAt time.Time  `json:"createdAt"`
	HandledAt *time.Time `json:"handledAt,omitempty"`
}

// AddContactMessage stores a message from the contact form.
func (s *Store) AddContactMessage(ctx context.Context, m ContactMessage) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO contact_messages(id,name,email,company,topic,message) VALUES ($1,$2,$3,$4,$5,$6)`,
		m.ID, m.Name, m.Email, m.Company, m.Topic, m.Message)
	return err
}

// ContactMessages lists the newest messages first.
func (s *Store) ContactMessages(ctx context.Context, limit int) ([]ContactMessage, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,name,email,company,topic,message,created_at,handled_at
		FROM contact_messages ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContactMessage{}
	for rows.Next() {
		var m ContactMessage
		if err := rows.Scan(&m.ID, &m.Name, &m.Email, &m.Company, &m.Topic, &m.Message, &m.CreatedAt, &m.HandledAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetContactHandled marks a message handled (or open again).
func (s *Store) SetContactHandled(ctx context.Context, id string, handled bool) (bool, error) {
	tag, err := s.DB.Exec(ctx, `UPDATE contact_messages SET handled_at = CASE WHEN $2 THEN now() ELSE NULL END WHERE id=$1`, id, handled)
	return tag.RowsAffected() == 1, err
}

// OperatorAdmins returns the e-mail addresses of the active platform admins.
func (s *Store) OperatorAdmins(ctx context.Context) ([]string, error) {
	rows, err := s.DB.Query(ctx, `SELECT u.email FROM users u JOIN orgs o ON o.id = u.org_id
		WHERE o.kind = 'operator' AND u.role = 'admin' AND u.disabled_at IS NULL AND NOT u.demo`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
