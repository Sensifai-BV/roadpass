-- Records whose ClickHouse row must be rebuilt although no new version was
-- written: a receipt changes what a delivery counts for (a load rejected at the
-- paver was not laid). The projector drains this after the seq-ordered feed.
CREATE TABLE reproject (
    id        text PRIMARY KEY,
    queued_at timestamptz NOT NULL DEFAULT now()
);
