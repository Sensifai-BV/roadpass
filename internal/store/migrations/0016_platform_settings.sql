-- Settings the platform manager changes in the console (today the outgoing mail
-- server, key 'mail', its value JSON). platform_settings already holds generated
-- values (0008); secrets never go in value: secret_ref points into the secrets store.
ALTER TABLE platform_settings
    ADD COLUMN secret_ref text,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN updated_by text;
