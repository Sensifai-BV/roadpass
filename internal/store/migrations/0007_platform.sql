-- The SaaS layer run by the platform manager: plans, one subscription per
-- organisation, and monthly invoices. Usage is metered from the data itself
-- (operational records created, active members); simulated data is never billed.
CREATE TABLE plans (
    id                text PRIMARY KEY,
    name              text NOT NULL,
    description       text NOT NULL DEFAULT '',
    price_cents       bigint NOT NULL CHECK (price_cents >= 0),
    currency          text NOT NULL DEFAULT 'EUR' CHECK (currency ~ '^[A-Z]{3}$'),
    seats             int NOT NULL,              -- -1 = unlimited
    records_per_month int NOT NULL,              -- -1 = unlimited (soft limit: flagged, not blocked)
    position          int NOT NULL DEFAULT 0,
    active            boolean NOT NULL DEFAULT true
);
INSERT INTO plans (id, name, description, price_cents, seats, records_per_month, position) VALUES
    ('starter',    'Starter',    'One site, a small team.',                          19000,  5,   2000, 1),
    ('business',   'Business',   'Several sites, full team, API and NGSI-LD.',       69000, 25,  25000, 2),
    ('enterprise', 'Enterprise', 'Unlimited, with a contract and invoiced yearly.',      0, -1,     -1, 3);

CREATE TABLE subscriptions (
    org_id               text PRIMARY KEY REFERENCES orgs(id) ON DELETE CASCADE,
    plan_id              text NOT NULL REFERENCES plans(id),
    status               text NOT NULL CHECK (status IN ('trial', 'active', 'past_due', 'cancelled')),
    trial_ends_at        timestamptz,
    current_period_start date NOT NULL DEFAULT date_trunc('month', now()),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    updated_by           text
);

CREATE TABLE invoices (
    id           text PRIMARY KEY,
    number       text UNIQUE,                     -- assigned when issued: RP-2026-0001
    org_id       text NOT NULL REFERENCES orgs(id),
    period       date NOT NULL,                   -- first day of the billed month
    plan_id      text NOT NULL REFERENCES plans(id),
    currency     text NOT NULL,
    amount_cents bigint NOT NULL,
    lines        jsonb NOT NULL,
    status       text NOT NULL CHECK (status IN ('draft', 'issued', 'paid', 'void')),
    issued_at    timestamptz,
    paid_at      timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, period)
);
CREATE SEQUENCE invoice_number_seq;

-- Usage metering: operational records an organisation created per month.
CREATE INDEX records_org_created ON records (org_id, created_at) WHERE provenance <> 'Simulated';
