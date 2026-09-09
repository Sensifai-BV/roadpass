-- Personal signing keys: every member has an Ed25519 key of their own, and every
-- action they take is signed with it as well as with the company key.
--   custody   platform = seed sealed here under SIGNING_MASTER_KEY (development,
--             simulated data); vault = non-exportable key in OpenBao Transit;
--             customer = the organisation holds the private key itself
--   vault_key / vault_version name the Transit key version that signs
ALTER TABLE signing_keys
    ADD COLUMN user_id       text REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN custody       text NOT NULL DEFAULT 'platform' CHECK (custody IN ('platform', 'vault', 'customer')),
    ADD COLUMN vault_key     text,
    ADD COLUMN vault_version int,
    ADD COLUMN revoke_reason text;
UPDATE signing_keys SET custody = 'customer' WHERE sealed_seed IS NULL;
ALTER TABLE signing_keys DROP CONSTRAINT IF EXISTS signing_keys_status_check;
ALTER TABLE signing_keys ADD CONSTRAINT signing_keys_status_check CHECK (status IN ('active', 'retired', 'revoked'));
ALTER TABLE signing_keys ADD CONSTRAINT signing_keys_custody_material CHECK (
    (custody = 'platform' AND sealed_seed IS NOT NULL) OR
    (custody = 'vault' AND vault_key IS NOT NULL AND vault_version IS NOT NULL) OR
    custody = 'customer');
CREATE UNIQUE INDEX signing_keys_one_active_per_user ON signing_keys (user_id) WHERE status = 'active' AND user_id IS NOT NULL;
DROP INDEX IF EXISTS signing_keys_org;
CREATE INDEX signing_keys_org ON signing_keys (org_id) WHERE status = 'active' AND user_id IS NULL;

ALTER TABLE records
    ADD COLUMN actor_key_id         text REFERENCES signing_keys(id),
    ADD COLUMN actor_signature      bytea,
    ADD COLUMN rcpt_actor_key_id    text REFERENCES signing_keys(id),
    ADD COLUMN rcpt_actor_signature bytea;

-- The worker revokes retired and revoked keys on the ledger too; signatures made
-- before the revocation stay valid there.
ALTER TABLE signing_keys ADD COLUMN ledger_revoke_tx_id text;
