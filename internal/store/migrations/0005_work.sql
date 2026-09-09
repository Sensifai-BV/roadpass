-- Who did the work, and what the receiver decided.
--   note           the signer's own note; part of the signed content hash
--   created_by     user or API key that submitted the record
--   received_by    user who signed the receipt
--   rcpt_decision  the intake review: accepted, accepted_with_note or rejected
--   rcpt_note      the receiver's note; covered by the receipt signature
ALTER TABLE records
    ADD COLUMN note          text NOT NULL DEFAULT '',
    ADD COLUMN created_by    text,
    ADD COLUMN received_by   text,
    ADD COLUMN rcpt_decision text CHECK (rcpt_decision IN ('accepted', 'accepted_with_note', 'rejected')),
    ADD COLUMN rcpt_note     text NOT NULL DEFAULT '';
CREATE INDEX records_created_by ON records (created_by, created_at DESC) WHERE created_by IS NOT NULL;
CREATE INDEX records_received_by ON records (received_by, received_at DESC) WHERE received_by IS NOT NULL;

-- An admin hands a waiting item (a record awaiting a duty) to one member.
CREATE TABLE work_assignments (
    record_id   text NOT NULL,
    duty        text NOT NULL CHECK (duty IN ('receive', 'produce', 'test', 'release', 'ship')),
    org_id      text NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id     text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    assigned_by text NOT NULL,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    note        text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    PRIMARY KEY (org_id, record_id, duty)
);
CREATE INDEX work_assignments_user ON work_assignments (user_id);
