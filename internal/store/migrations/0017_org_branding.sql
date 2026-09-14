-- What an organisation prints on its reports and passports: legal name,
-- address and contact, a footer line, and a logo (PNG or JPEG, at most 300 kB).
ALTER TABLE orgs
    ADD COLUMN legal_name    text NOT NULL DEFAULT '',
    ADD COLUMN address       text NOT NULL DEFAULT '',
    ADD COLUMN contact_email text NOT NULL DEFAULT '',
    ADD COLUMN phone         text NOT NULL DEFAULT '',
    ADD COLUMN report_footer text NOT NULL DEFAULT '';

CREATE TABLE org_logos (
    org_id       text PRIMARY KEY REFERENCES orgs(id) ON DELETE CASCADE,
    content_type text NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg')),
    data         bytea NOT NULL CHECK (octet_length(data) <= 307200),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
