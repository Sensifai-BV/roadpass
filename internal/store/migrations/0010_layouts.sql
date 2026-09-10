-- Each person's own Overview: widgets, sizes, names, positions and chart styles.
-- Without one they see their team's default (a dashboard an admin published to
-- their role or duty), else the built-in layout.
CREATE TABLE user_layouts (
    user_id    text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    page       text NOT NULL CHECK (page IN ('overview')),
    widgets    jsonb NOT NULL CHECK (jsonb_typeof(widgets) = 'array'),
    version    int NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, page)
);
