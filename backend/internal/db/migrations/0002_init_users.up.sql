-- 0002_init_users
CREATE TABLE users (
    id                   BIGSERIAL PRIMARY KEY,
    username             TEXT NOT NULL UNIQUE,
    password_hash        TEXT NOT NULL,
    email                TEXT UNIQUE,
    role                 TEXT NOT NULL DEFAULT 'user',  -- owner / admin / user
    traffic_limit_bytes  BIGINT NOT NULL DEFAULT 0,     -- 0 = 不限
    expire_at            TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE invite_codes (
    id            BIGSERIAL PRIMARY KEY,
    code          TEXT NOT NULL UNIQUE,
    max_uses      INT NOT NULL DEFAULT 1,
    used_count    INT NOT NULL DEFAULT 0,
    expires_at    TIMESTAMPTZ,
    node_group_id BIGINT,
    created_by    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
