-- Dashboards an organisation's admins design for their own team. The widget
-- layout is a JSON document validated by the API; audience lists who sees the
-- dashboard as "role:<role>" or "duty:<duty>" tokens, or "all".
CREATE TABLE dashboards (
    id          text PRIMARY KEY,
    org_id      text NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    description text NOT NULL DEFAULT '' CHECK (length(description) <= 280),
    audience    text[] NOT NULL DEFAULT '{}',
    position    int NOT NULL DEFAULT 0,
    widgets     jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(widgets) = 'array'),
    version     int NOT NULL DEFAULT 1,
    created_by  text NOT NULL,
    updated_by  text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dashboards_org ON dashboards(org_id, position);
