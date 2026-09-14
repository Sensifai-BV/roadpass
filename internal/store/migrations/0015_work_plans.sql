-- Planned work: a production run, shipment, delivery or road-section paving a
-- company expects to do. Plans are notes for the companies involved, not
-- evidence: they are never signed, hashed or sealed. When the real record
-- arrives the plan is linked to it and marked done.
--   record_type   the record the plan expects (a lot type, shipment, delivery, road_section)
--   counterparty  the receiving company (shipments, deliveries) or the contractor's client
--   owner_org_id  the road owner (road sections)
CREATE TABLE work_plans (
    id            text PRIMARY KEY,
    org_id        text NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    record_type   text NOT NULL CHECK (record_type IN ('reject_lot','recycled_lot','additive_batch','asphalt_lot',
                                                       'shipment','delivery','road_section')),
    title         text NOT NULL DEFAULT '' CHECK (length(title) <= 160),
    site_id       text REFERENCES sites(id),
    counterparty  text REFERENCES orgs(id),
    owner_org_id  text REFERENCES orgs(id),
    starts_at     timestamptz NOT NULL,
    ends_at       timestamptz,
    mass_kg       double precision NOT NULL DEFAULT 0 CHECK (mass_kg >= 0),
    note          text NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    status        text NOT NULL DEFAULT 'planned' CHECK (status IN ('planned','done','cancelled')),
    record_id     text,
    done_at       timestamptz,
    done_by       text,
    created_by    text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_by    text NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_at IS NULL OR ends_at >= starts_at)
);
CREATE INDEX work_plans_org_time   ON work_plans (org_id, starts_at);
CREATE INDEX work_plans_cp_time    ON work_plans (counterparty, starts_at) WHERE counterparty IS NOT NULL;
CREATE INDEX work_plans_owner_time ON work_plans (owner_org_id, starts_at) WHERE owner_org_id IS NOT NULL;
CREATE UNIQUE INDEX work_plans_record ON work_plans (record_id) WHERE record_id IS NOT NULL;
