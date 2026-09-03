-- 0005_init_traffic
CREATE TABLE traffic_records (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id      BIGINT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    inbound_id   BIGINT NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
    up_bytes     BIGINT NOT NULL DEFAULT 0,
    down_bytes   BIGINT NOT NULL DEFAULT 0,
    period_start TIMESTAMPTZ NOT NULL,
    UNIQUE (user_id, node_id, inbound_id, period_start)
);

CREATE INDEX idx_traffic_user_period ON traffic_records (user_id, period_start);
CREATE INDEX idx_traffic_node_period ON traffic_records (node_id, period_start);
