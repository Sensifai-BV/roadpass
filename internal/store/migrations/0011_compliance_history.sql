-- Compliance, second pass.
--  * Attestations become an append-only history: each statement is signed with
--    the member's personal key and is never changed; a newer one supersedes it.
--  * Limits can be switched off without losing them.
--  * One report schedule per organisation.
--  * Data-protection requests (GDPR access and erasure) are logged.

ALTER TABLE compliance_attestations DROP CONSTRAINT compliance_attestations_pkey;
ALTER TABLE compliance_attestations
    ADD COLUMN id      text,
    ADD COLUMN period  text NOT NULL DEFAULT '',   -- '' or a month, e.g. 2026-09 (report sign-off)
    ADD COLUMN message text NOT NULL DEFAULT '';   -- the exact signed text
UPDATE compliance_attestations SET id = 'att_' || md5(org_id || requirement_id || attested_at::text) WHERE id IS NULL;
ALTER TABLE compliance_attestations ALTER COLUMN id SET NOT NULL;
ALTER TABLE compliance_attestations ADD PRIMARY KEY (id);
CREATE INDEX compliance_attestations_req ON compliance_attestations (org_id, requirement_id, period, attested_at DESC);

CREATE FUNCTION compliance_attestations_frozen() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'attestations are append-only';
END $$;
CREATE TRIGGER compliance_attestations_no_update BEFORE UPDATE ON compliance_attestations
    FOR EACH ROW EXECUTE FUNCTION compliance_attestations_frozen();

ALTER TABLE compliance_limits
    ADD COLUMN enabled    boolean NOT NULL DEFAULT true,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

CREATE UNIQUE INDEX compliance_schedules_org ON compliance_schedules (org_id);

CREATE TABLE privacy_requests (
    id           text PRIMARY KEY,
    org_id       text NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    subject_user text NOT NULL,                  -- pseudonymous user id; kept after erasure
    kind         text NOT NULL CHECK (kind IN ('export', 'erase')),
    requested_by text NOT NULL,
    at           timestamptz NOT NULL DEFAULT now(),
    detail       jsonb
);
CREATE INDEX privacy_requests_org ON privacy_requests (org_id, at DESC);
