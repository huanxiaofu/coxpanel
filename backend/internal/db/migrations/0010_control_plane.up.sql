-- 0010 control-plane drafts and explicit deployment snapshots
CREATE TABLE topology_drafts (
    node_id    BIGINT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    revision   BIGINT NOT NULL DEFAULT 1,
    edges      JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (jsonb_typeof(edges) = 'array')
);

CREATE TABLE topology_deployments (
    node_id        BIGINT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    version        TEXT NOT NULL,
    schema_version TEXT NOT NULL,
    draft_revision BIGINT NOT NULL DEFAULT 0,
    topology       JSONB NOT NULL,
    rendered       BYTEA NOT NULL,
    deployed_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_topology_deployments_version ON topology_deployments (version);

CREATE TABLE topology_previews (
    node_id        BIGINT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    version        TEXT NOT NULL,
    draft_revision BIGINT NOT NULL DEFAULT 0,
    material_revision BIGINT NOT NULL DEFAULT 1,
    material_hash  TEXT NOT NULL,
    topology       JSONB NOT NULL,
    rendered       BYTEA NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_topology_previews_version ON topology_previews (version);

CREATE TABLE topology_material_state (
    id       BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    revision BIGINT NOT NULL DEFAULT 1
);

INSERT INTO topology_material_state (id, revision)
VALUES (TRUE, 1)
ON CONFLICT (id) DO NOTHING;
