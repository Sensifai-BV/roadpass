-- Messages from the public contact form. Anyone can send one; only the platform
-- operator's admins read them. Kept as a small inbox, not a ticketing system.
CREATE TABLE contact_messages (
    id         text PRIMARY KEY,
    name       text NOT NULL,
    email      text NOT NULL,
    company    text NOT NULL DEFAULT '',
    topic      text NOT NULL DEFAULT 'general',
    message    text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    handled_at timestamptz
);
CREATE INDEX contact_messages_open ON contact_messages (created_at DESC);
