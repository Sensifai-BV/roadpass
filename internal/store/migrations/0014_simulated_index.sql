-- Lets /meta answer "is there any simulated data?" without scanning records.
CREATE INDEX IF NOT EXISTS records_simulated ON records ((1)) WHERE provenance = 'Simulated';
