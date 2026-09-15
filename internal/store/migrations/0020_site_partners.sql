-- Sites are each organisation's private supply-chain address book: its own
-- plants and depots (partner = '') and the partner companies' sites it ships to
-- (partner = the company's name). Nothing here is visible to other customers.
ALTER TABLE sites
    ADD COLUMN kind        text NOT NULL DEFAULT '',
    ADD COLUMN kind_other  text NOT NULL DEFAULT '',
    ADD COLUMN partner     text NOT NULL DEFAULT '',
    ADD COLUMN country     text NOT NULL DEFAULT '',
    ADD COLUMN address     text NOT NULL DEFAULT '',
    ADD COLUMN archived_at timestamptz;
UPDATE sites s SET kind = o.kind, country = o.country FROM orgs o WHERE o.id = s.org_id AND s.kind = '';

-- A planned shipment names the receiving site from the planner's address book.
ALTER TABLE work_plans ADD COLUMN to_site_id text REFERENCES sites(id);
