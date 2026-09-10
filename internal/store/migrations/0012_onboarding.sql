-- Onboarding: an organisation's admin can put the "get started" guide away
-- before every step is done (for example when data arrive only later).
ALTER TABLE orgs ADD COLUMN setup_dismissed_at timestamptz;
