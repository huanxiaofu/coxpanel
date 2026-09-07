-- 0007 agent_credentials
CREATE TABLE agent_credentials (
    node_id         BIGINT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    credential_hash TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
