-- 0003_init_groups
CREATE TABLE node_groups (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE node_group_members (
    group_id BIGINT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    node_id  BIGINT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, node_id)
);
