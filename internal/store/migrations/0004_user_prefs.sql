-- Personal preferences: the IANA time zone dates and times are shown in
-- ('' = the browser's own).
ALTER TABLE users ADD COLUMN timezone text NOT NULL DEFAULT '';
