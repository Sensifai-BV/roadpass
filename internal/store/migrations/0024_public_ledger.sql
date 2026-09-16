-- Public ledger explorer: an organisation's seals and blocks are shown on the
-- public /explorer page only when its admin turns this on. The demo workspace is
-- public from the start.
ALTER TABLE orgs ADD COLUMN public_ledger boolean NOT NULL DEFAULT false;
UPDATE orgs SET public_ledger = true WHERE demo;
