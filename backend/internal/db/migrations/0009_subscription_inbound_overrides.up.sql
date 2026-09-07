-- 0009 subscription overrides scoped to one node inbound
CREATE TABLE subscription_inbound_overrides (
    id              BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    node_id         BIGINT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    inbound_id      BIGINT NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
    display_name    TEXT,
    sort_order      INT NOT NULL DEFAULT 0,
    icon            TEXT,
    params          JSONB NOT NULL DEFAULT '{}',
    proxy_group     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (subscription_id, node_id, inbound_id)
);

CREATE INDEX idx_subscription_inbound_overrides_subscription
    ON subscription_inbound_overrides (subscription_id);
