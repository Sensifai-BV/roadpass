-- Sections of the chain a member works in. A company that does several steps
-- (sorting and recycling, or mixing asphalt and laying it) assigns each person
-- to theirs: the sorter signs sorting records only. Empty = every section of
-- the company (admins, reporters, and members of single-step companies).
ALTER TABLE users       ADD COLUMN steps text[] NOT NULL DEFAULT '{}';
ALTER TABLE invitations ADD COLUMN steps text[] NOT NULL DEFAULT '{}';
