-- Dedicated seals: every seal (and so every ledger block, one seal transaction
-- per block) holds the records of one organisation only. Seals made before this
-- mixed organisations and keep org_id NULL ("shared").
ALTER TABLE seals ADD COLUMN org_id text REFERENCES orgs(id);
UPDATE seals s SET org_id = m.org_id
FROM (SELECT seal_id, min(org_id) AS org_id FROM records WHERE seal_id IS NOT NULL
      GROUP BY seal_id HAVING count(DISTINCT org_id) = 1) m
WHERE s.id = m.seal_id;
CREATE INDEX seals_org ON seals(org_id, created_at DESC);
