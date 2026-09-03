-- 0001_init_nodes
CREATE TABLE nodes (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    type          TEXT NOT NULL DEFAULT 'managed',  -- managed / external
    public_ip     TEXT,
    easy_ip       TEXT,
    ssh_host      TEXT,
    ssh_user      TEXT,
    ssh_port      INT DEFAULT 22,
    core_version  TEXT,
    status        TEXT NOT NULL DEFAULT 'offline',  -- online/offline/maintenance
    last_seen_at  TIMESTAMPTZ,
    ext_protocol  TEXT,
    ext_params    JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE inbounds (
    id             BIGSERIAL PRIMARY KEY,
    node_id        BIGINT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    protocol       TEXT NOT NULL,               -- vless-reality / shadowsocks / hysteria2
    role           TEXT NOT NULL DEFAULT 'entry', -- entry / landing / relay
    listen_addr    TEXT NOT NULL DEFAULT '::',
    listen_port    INT NOT NULL,
    config         JSONB NOT NULL DEFAULT '{}',
    min_client_ver TEXT NOT NULL DEFAULT '1.8.2',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE edges (
    id               BIGSERIAL PRIMARY KEY,
    from_inbound_id  BIGINT NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
    to_node_id       BIGINT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    to_inbound_id    BIGINT NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
    weight           INT NOT NULL DEFAULT 1,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
