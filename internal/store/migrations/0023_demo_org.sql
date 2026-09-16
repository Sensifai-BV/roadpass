-- The public demo workspace: its data is rebuilt every day and the API limits what
-- visitors can write (see internal/demo and internal/api/demo.go).
ALTER TABLE orgs ADD COLUMN demo boolean NOT NULL DEFAULT false;
