-- 0006 用户协议凭据：每个用户在每个 entry 入站上拥有独立凭据
-- vless: uuid；ss: password；hy2: password
CREATE TABLE user_credentials (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    inbound_id BIGINT NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
    credential TEXT NOT NULL,              -- uuid 或 password
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, inbound_id)
);

CREATE INDEX idx_credentials_inbound ON user_credentials (inbound_id);
