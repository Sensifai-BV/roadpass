-- Alerts and notifications. Rules watch the chain (a metric crossing a
-- threshold, a check finding, a late or rejected shipment, missing telemetry, a
-- seal backlog, a plan limit); a firing rule notifies people in-app, by browser
-- push, and through the organisation's channels (e-mail, webhook, Slack, Teams,
-- SMS, Telegram). Lifecycle per (rule, key): pending → firing → resolved, with
-- hold time, reminders, escalation, acknowledge, snooze and assignment.

CREATE TABLE alert_channels (
    id              text PRIMARY KEY,
    org_id          text NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    type            text NOT NULL CHECK (type IN ('email', 'webhook', 'slack', 'teams', 'sms', 'telegram')),
    name            text NOT NULL,
    config          jsonb NOT NULL DEFAULT '{}',   -- non-secret settings (address, number, chat id, URL host)
    secret_ref      text,                          -- where the secret lives: vault:<path> or local:<id>
    verified        boolean NOT NULL DEFAULT false,
    verify_hash     bytea,
    verify_expires  timestamptz,
    verify_attempts int NOT NULL DEFAULT 0,
    last_status     text,
    last_error      text,
    last_sent_at    timestamptz,
    created_by      text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX alert_channels_org ON alert_channels (org_id);

-- Secrets of channels when no vault is configured (development only).
CREATE TABLE local_secrets (
    id     text PRIMARY KEY,
    sealed bytea NOT NULL
);

CREATE TABLE alert_rules (
    id              text PRIMARY KEY,
    org_id          text NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name            text NOT NULL,
    kind            text NOT NULL CHECK (kind IN ('metric', 'finding', 'receipt_overdue', 'rejected', 'telemetry_gap', 'seal_backlog', 'plan_limit')),
    params          jsonb NOT NULL DEFAULT '{}',
    severity        text NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    audience        text[] NOT NULL DEFAULT '{}',  -- role:<r>, duty:<d>, user:<id>, all
    channel_ids     text[] NOT NULL DEFAULT '{}',
    for_minutes     int NOT NULL DEFAULT 0 CHECK (for_minutes BETWEEN 0 AND 1440),
    repeat_minutes  int NOT NULL DEFAULT 0 CHECK (repeat_minutes = 0 OR repeat_minutes BETWEEN 15 AND 10080),
    notify_resolved boolean NOT NULL DEFAULT true,
    escalation      jsonb NOT NULL DEFAULT '[]',   -- [{afterMinutes, channelIds, audience}] up to 4
    enabled         boolean NOT NULL DEFAULT true,
    version         int NOT NULL DEFAULT 1,
    created_by      text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX alert_rules_org ON alert_rules (org_id) WHERE enabled;

CREATE TABLE alert_state (
    rule_id       text NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    key           text NOT NULL,                  -- what fired: a site, a shipment, a finding…
    state         text NOT NULL CHECK (state IN ('pending', 'firing')),
    since         timestamptz NOT NULL,
    fired_at      timestamptz,
    last_notified timestamptz,
    value         double precision,
    detail        jsonb,
    event_id      bigint,
    ack_by        text,
    ack_at        timestamptz,
    snooze_until  timestamptz,
    assigned_to   text,
    esc_level     int NOT NULL DEFAULT 0,
    PRIMARY KEY (rule_id, key)
);

CREATE TABLE alert_events (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id      text NOT NULL,
    rule_id     text NOT NULL,
    key         text NOT NULL,
    state       text NOT NULL CHECK (state IN ('firing', 'reminder', 'escalated', 'resolved')),
    severity    text NOT NULL,
    title       text NOT NULL,
    body        text NOT NULL,
    link        text,
    value       double precision,
    at          timestamptz NOT NULL DEFAULT now(),
    deliveries  jsonb NOT NULL DEFAULT '[]',      -- [{channel, type, ok, error}]
    ack_by      text,
    ack_at      timestamptz,
    assigned_to text
);
CREATE INDEX alert_events_org ON alert_events (org_id, at DESC);
CREATE INDEX alert_events_rule ON alert_events (rule_id, key, at DESC);

CREATE TABLE alert_maintenance (
    id        text PRIMARY KEY,
    org_id    text NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    starts_at timestamptz NOT NULL,
    ends_at   timestamptz NOT NULL CHECK (ends_at > starts_at),
    rule_ids  text[] NOT NULL DEFAULT '{}',       -- empty = every rule
    note      text NOT NULL DEFAULT '',
    created_by text NOT NULL
);

-- Where the engine left off for event-driven rules (findings, rejections).
CREATE TABLE alert_cursors (
    name text PRIMARY KEY,
    at   timestamptz NOT NULL
);

-- In-app notifications, one row per recipient.
CREATE TABLE notifications (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id     text NOT NULL,
    user_id    text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       text NOT NULL,                     -- alert | assignment | org | key | billing | system
    severity   text NOT NULL DEFAULT 'info',
    title      text NOT NULL,
    body       text NOT NULL DEFAULT '',
    link       text,
    event_id   bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    read_at    timestamptz
);
CREATE INDEX notifications_user ON notifications (user_id, created_at DESC);
CREATE INDEX notifications_unread ON notifications (user_id) WHERE read_at IS NULL;

-- Each person's choices: browser push, e-mail, quiet hours, muted kinds.
CREATE TABLE notification_prefs (
    user_id      text PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    browser      boolean NOT NULL DEFAULT true,
    email        text NOT NULL DEFAULT 'off' CHECK (email IN ('off', 'instant', 'daily')),
    min_severity text NOT NULL DEFAULT 'info' CHECK (min_severity IN ('info', 'warning', 'critical')),
    quiet_from   int CHECK (quiet_from BETWEEN 0 AND 1439),   -- minutes after midnight, user's time zone
    quiet_to     int CHECK (quiet_to BETWEEN 0 AND 1439),
    muted        text[] NOT NULL DEFAULT '{}',
    digest_sent  date
);

-- Browser push subscriptions (Web Push, VAPID).
CREATE TABLE push_subscriptions (
    id         text PRIMARY KEY,
    user_id    text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint   text NOT NULL UNIQUE,
    p256dh     text NOT NULL,
    auth       text NOT NULL,
    user_agent text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    last_ok_at timestamptz,
    failures   int NOT NULL DEFAULT 0
);
CREATE INDEX push_subscriptions_user ON push_subscriptions (user_id);

-- Platform settings that are generated once (the VAPID key pair's public half;
-- the private half lives in the vault or sealed locally).
CREATE TABLE platform_settings (
    key   text PRIMARY KEY,
    value text NOT NULL
);
