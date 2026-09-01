-- ROADPASS system of record. ClickHouse is a projection of this; Redis holds nothing
-- that cannot be rebuilt from it.

CREATE TABLE orgs (
    id           text PRIMARY KEY,
    name         text NOT NULL,
    kind         text NOT NULL CHECK (kind IN ('sorting_plant','recycler','additive_producer','asphalt_plant',
                                               'contractor','road_owner','lab','auditor','reporter','operator')),
    -- every role the organisation plays in the chain (an asphalt company may also lay roads)
    kinds        text[] NOT NULL,
    tax_id       text,
    city         text NOT NULL DEFAULT '',
    province     text NOT NULL DEFAULT '',
    region       text NOT NULL DEFAULT '',
    country      text NOT NULL DEFAULT 'ES',
    lat          double precision,
    lon          double precision,
    description  text NOT NULL DEFAULT '',
    provenance   text NOT NULL,
    -- Self-service: an organisation registers itself; the operator verifies it (KYB)
    -- before its signing key is put on the ledger and its records are sealed.
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','verified','suspended')),
    website      text NOT NULL DEFAULT '',
    verified_at  timestamptz,
    verified_by  text,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sites (
    id          text PRIMARY KEY,
    org_id      text NOT NULL REFERENCES orgs(id),
    name        text NOT NULL,
    city        text NOT NULL DEFAULT '',
    province    text NOT NULL DEFAULT '',
    region      text NOT NULL DEFAULT '',
    lat         double precision,
    lon         double precision,
    capacity_t  double precision NOT NULL DEFAULT 0,
    equipment   text NOT NULL DEFAULT ''
);
CREATE INDEX sites_org ON sites(org_id);

CREATE TABLE users (
    id             text PRIMARY KEY,
    org_id         text NOT NULL REFERENCES orgs(id),
    email          text NOT NULL,
    name           text NOT NULL,
    role           text NOT NULL CHECK (role IN ('admin','editor','viewer')),
    -- jobs inside the organisation's step: receive, produce, test, release, ship
    duties         text[] NOT NULL DEFAULT '{}',
    password_hash  text NOT NULL,
    demo           boolean NOT NULL DEFAULT false,
    disabled_at    timestamptz,
    last_login_at  timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email ON users(lower(email));
CREATE INDEX users_org ON users(org_id);

-- Team invitations: an organisation admin invites colleagues by e-mail. Only the
-- token's hash is stored; the link is shown once to the admin.
CREATE TABLE invitations (
    id          text PRIMARY KEY,
    org_id      text NOT NULL REFERENCES orgs(id),
    email       text NOT NULL,
    role        text NOT NULL CHECK (role IN ('admin','editor','viewer')),
    duties      text[] NOT NULL DEFAULT '{}',
    token_hash  bytea NOT NULL UNIQUE,
    invited_by  text NOT NULL REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    accepted_at timestamptz,
    revoked_at  timestamptz
);
CREATE INDEX invitations_org ON invitations(org_id) WHERE accepted_at IS NULL AND revoked_at IS NULL;

CREATE TABLE api_keys (
    id            text PRIMARY KEY,
    org_id        text NOT NULL REFERENCES orgs(id),
    name          text NOT NULL,
    prefix        text NOT NULL,              -- shown in the UI: rpk_live_ab12…
    hash          bytea NOT NULL UNIQUE,      -- sha256 of the full key
    scopes        text[] NOT NULL,
    duties        text[] NOT NULL DEFAULT '{receive,produce,test,release,ship}',
    created_by    text REFERENCES users(id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz,
    revoked_at    timestamptz
);
CREATE INDEX api_keys_org ON api_keys(org_id);

CREATE TABLE signing_keys (
    id            text PRIMARY KEY,           -- <org>:k<n>
    org_id        text NOT NULL REFERENCES orgs(id),
    public_key    bytea NOT NULL,
    sealed_seed   bytea,                      -- AES-GCM under SIGNING_MASTER_KEY; NULL for customer-held keys
    status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
    ledger_tx_id  text,                       -- RegisterSigner transaction, NULL until registered
    created_at    timestamptz NOT NULL DEFAULT now(),
    revoked_at    timestamptz
);
CREATE INDEX signing_keys_org ON signing_keys(org_id) WHERE status = 'active';

-- Every record version. (id, version) is the identity; seq is the global ingest
-- order the sealer and the ClickHouse projector follow.
CREATE TABLE records (
    seq            bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    id             text NOT NULL,
    version        int  NOT NULL CHECK (version >= 1),
    latest         boolean NOT NULL DEFAULT true,
    prev_hash      text,
    type           text NOT NULL,
    org_id         text NOT NULL REFERENCES orgs(id),
    site_id        text REFERENCES sites(id),
    counterparty   text REFERENCES orgs(id),
    ref            text NOT NULL,
    occurred_at    timestamptz NOT NULL,
    mass_kg        double precision NOT NULL DEFAULT 0,
    inputs         jsonb,
    body           jsonb NOT NULL,
    provenance     text NOT NULL,
    content_hash   text NOT NULL,
    dup_hash       text NOT NULL,
    status         text NOT NULL DEFAULT 'ok' CHECK (status IN ('ok','flagged','blocked','accepted')),
    key_id         text REFERENCES signing_keys(id),
    signature      bytea,
    signed_at      timestamptz,
    -- shipment receipt, signed by the receiver (signing.ReceiptMessage)
    received_at       timestamptz,
    received_mass_kg  double precision,
    rcpt_key_id       text REFERENCES signing_keys(id),
    rcpt_signature    bytea,
    seal_id        text,
    leaf_index     int,
    ingest_id      text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (id, version)
);
-- One latest version per id; the external reference is unique per company and type.
CREATE UNIQUE INDEX records_latest   ON records(id) WHERE latest;
CREATE UNIQUE INDEX records_ref      ON records(org_id, type, ref) WHERE latest;
CREATE INDEX records_type_time       ON records(type, occurred_at DESC) WHERE latest;
CREATE INDEX records_org_time        ON records(org_id, occurred_at DESC) WHERE latest;
CREATE INDEX records_cp_time         ON records(counterparty, occurred_at DESC) WHERE latest AND counterparty IS NOT NULL;
CREATE INDEX records_hash            ON records(content_hash);
CREATE INDEX records_dup             ON records(dup_hash) WHERE latest;
CREATE INDEX records_unsealed        ON records(seq) WHERE seal_id IS NULL AND status <> 'blocked';
CREATE INDEX records_seal            ON records(seal_id, leaf_index);
CREATE INDEX records_status          ON records(status) WHERE latest AND status <> 'ok';

-- Material lineage: parent was consumed in, carried by, or laid as child.
CREATE TABLE links (
    parent_id  text NOT NULL,
    child_id   text NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('input','shipped','delivered','laid','tested','accepts','releases')),
    mass_kg    double precision NOT NULL DEFAULT 0,
    PRIMARY KEY (parent_id, child_id, kind)
);
CREATE INDEX links_child ON links(child_id);

-- Precomputed passport figures: opening a passport is one row, not a graph walk.
CREATE TABLE passport_summary (
    record_id          text PRIMARY KEY,
    recycled_kg        double precision NOT NULL DEFAULT 0,
    origin_orgs        text[] NOT NULL DEFAULT '{}',
    origin_regions     text[] NOT NULL DEFAULT '{}',
    upstream_records   int NOT NULL DEFAULT 0,
    co2e_avoided_kg    double precision NOT NULL DEFAULT 0,
    co2e_footprint_kg  double precision NOT NULL DEFAULT 0,
    transport_tkm      double precision NOT NULL DEFAULT 0,
    open_findings      int NOT NULL DEFAULT 0,
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE findings (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    record_id    text NOT NULL,
    code         text NOT NULL,
    severity     text NOT NULL CHECK (severity IN ('blocker','warning')),
    message      text NOT NULL,
    fix          text NOT NULL,
    detail       jsonb,
    status       text NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','accepted')),
    org_id       text NOT NULL,                -- owner of the record (visibility)
    created_at   timestamptz NOT NULL DEFAULT now(),
    closed_at    timestamptz,
    closed_by    text,
    close_reason text,
    UNIQUE (record_id, code)
);
CREATE INDEX findings_open ON findings(org_id, created_at DESC) WHERE status = 'open';

CREATE TABLE seals (
    id           text PRIMARY KEY,
    merkle_root  text NOT NULL,
    leaf_count   int NOT NULL,
    first_seq    bigint NOT NULL,
    last_seq     bigint NOT NULL,
    provenance   text NOT NULL,
    status       text NOT NULL CHECK (status IN ('pending','sealed','failed')),
    tx_id        text,
    block_number bigint,
    error        text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    sealed_at    timestamptz
);
CREATE INDEX seals_status ON seals(status, created_at);

CREATE TABLE ingests (
    id               text PRIMARY KEY,
    org_id           text NOT NULL REFERENCES orgs(id),
    channel          text NOT NULL CHECK (channel IN ('api','ngsi-ld','csv','seed')),
    record_type      text,
    status           text NOT NULL CHECK (status IN ('queued','running','done','failed')),
    total            int NOT NULL DEFAULT 0,
    accepted         int NOT NULL DEFAULT 0,
    rejected         int NOT NULL DEFAULT 0,
    errors           jsonb,
    idempotency_key  text,
    created_by       text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    finished_at      timestamptz
);
CREATE UNIQUE INDEX ingests_idem ON ingests(org_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX ingests_org ON ingests(org_id, created_at DESC);

-- Cursors of background loops (projector, checker).
CREATE TABLE cursors (
    name     text PRIMARY KEY,
    position bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Who did what: every write through the UI or API.
CREATE TABLE audit_log (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at         timestamptz NOT NULL DEFAULT now(),
    org_id     text,
    actor      text NOT NULL,          -- user id, api key id, or system loop
    action     text NOT NULL,
    subject    text,
    detail     jsonb
);
CREATE INDEX audit_org ON audit_log(org_id, at DESC);

-- Human record ids (ASL-25-004213): one counter per prefix and year.
CREATE TABLE id_counters (
    key  text PRIMARY KEY,   -- e.g. ASL-25
    n    bigint NOT NULL
);

-- Large CSV imports are stored here in chunks and processed by workers; the Redis
-- stream only carries the ingest id.
CREATE TABLE ingest_chunks (
    ingest_id  text NOT NULL REFERENCES ingests(id) ON DELETE CASCADE,
    chunk      int  NOT NULL,
    data       jsonb NOT NULL,
    done       boolean NOT NULL DEFAULT false,
    PRIMARY KEY (ingest_id, chunk)
);
