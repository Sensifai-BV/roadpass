-- A public passport link carries a random token, never the passport id (ids are
-- sequential and guessable). One active token per passport; rotating or stopping
-- sharing revokes it, and the old link stops working at once.
CREATE TABLE passport_shares (
    token      text PRIMARY KEY,
    record_id  text NOT NULL,
    org_id     text NOT NULL REFERENCES orgs(id),
    created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);
CREATE UNIQUE INDEX passport_shares_active ON passport_shares (record_id) WHERE revoked_at IS NULL;
