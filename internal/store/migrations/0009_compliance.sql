-- Compliance. Two parts:
--  1. Limit reporting (as in AquaSense): limits on measures, an exceedance log,
--     data completeness, calibration notes, and a monthly signed PDF report that
--     can be e-mailed on a schedule.
--  2. An EU regulatory register: requirements from ESPR (Digital Product
--     Passport), the Waste Framework Directive (end-of-waste), PPWR, CPR /
--     EN 13108, CSRD ESRS E5, the EU Taxonomy and GDPR, each with automated
--     evidence from the data or a signed attestation by a member.

CREATE TABLE compliance_limits (
    id         text PRIMARY KEY,
    org_id     text NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name       text NOT NULL,
    source     text NOT NULL,                 -- analytics catalogue source
    measure    text NOT NULL,                 -- analytics catalogue measure
    types      text[] NOT NULL DEFAULT '{}',  -- record types (records source)
    site_id    text,
    basis      text NOT NULL CHECK (basis IN ('instant', 'daily_mean', 'monthly_mean')),
    min_value  double precision,
    max_value  double precision,
    note       text NOT NULL DEFAULT '',
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (min_value IS NOT NULL OR max_value IS NOT NULL),
    CHECK (min_value IS NULL OR max_value IS NULL OR min_value < max_value)
);
CREATE INDEX compliance_limits_org ON compliance_limits (org_id);

CREATE TABLE compliance_calibrations (
    id         text PRIMARY KEY,
    org_id     text NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    site_id    text,
    parameter  text NOT NULL,
    at         timestamptz NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('calibration', 'check', 'maintenance', 'replacement')),
    by_who     text NOT NULL,
    note       text NOT NULL DEFAULT '',
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX compliance_calibrations_org ON compliance_calibrations (org_id, at DESC);

CREATE TABLE compliance_schedules (
    id           text PRIMARY KEY,
    org_id       text NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    recipients   text[] NOT NULL,
    day          int NOT NULL CHECK (day BETWEEN 1 AND 28),
    hour         int NOT NULL CHECK (hour BETWEEN 0 AND 23),
    timezone     text NOT NULL DEFAULT 'UTC',
    enabled      boolean NOT NULL DEFAULT true,
    last_period  date,
    last_run_at  timestamptz,
    last_status  text,
    last_message text,
    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- A member's signed statement on a requirement the data cannot show by itself.
CREATE TABLE compliance_attestations (
    org_id         text NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    requirement_id text NOT NULL,
    status         text NOT NULL CHECK (status IN ('met', 'partial', 'not_met', 'not_applicable')),
    note           text NOT NULL,
    evidence_url   text NOT NULL DEFAULT '',
    attested_by    text NOT NULL,
    attested_at    timestamptz NOT NULL DEFAULT now(),
    key_id         text,
    signature      bytea,
    PRIMARY KEY (org_id, requirement_id)
);
