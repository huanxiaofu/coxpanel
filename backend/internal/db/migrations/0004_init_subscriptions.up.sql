-- 0004_init_subscriptions
CREATE TABLE subscriptions (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    token         TEXT NOT NULL UNIQUE,      -- URL /sub/<token>
    format        TEXT NOT NULL DEFAULT 'mihomo',  -- mihomo / sing-box / base64 / v2rayn
    node_group_id BIGINT REFERENCES node_groups(id) ON DELETE SET NULL,
    template_id   BIGINT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE subscription_node_overrides (
    id              BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    node_id         BIGINT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    display_name    TEXT,
    sort_order      INT NOT NULL DEFAULT 0,
    icon            TEXT,
    params          JSONB NOT NULL DEFAULT '{}',
    proxy_group     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (subscription_id, node_id)
);

CREATE TABLE templates (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    format     TEXT NOT NULL,
    definition JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
