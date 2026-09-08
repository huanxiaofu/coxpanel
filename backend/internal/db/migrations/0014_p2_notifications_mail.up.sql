ALTER TABLE users ADD COLUMN email_verified_at TIMESTAMPTZ, ADD COLUMN email_revision BIGINT NOT NULL DEFAULT 1, ADD COLUMN quota_revision BIGINT NOT NULL DEFAULT 1;
CREATE TABLE alert_rules (
    id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('traffic_limit','expiration')), enabled BOOLEAN NOT NULL DEFAULT TRUE,
    scope_type TEXT NOT NULL CHECK(scope_type IN ('all','group','user')), scope_id BIGINT, thresholds JSONB NOT NULL,
    channel TEXT NOT NULL DEFAULT 'email' CHECK(channel='email'), revision BIGINT NOT NULL DEFAULT 1, created_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), CHECK ((scope_type='all')=(scope_id IS NULL))
);
INSERT INTO alert_rules(name,kind,scope_type,thresholds,created_by) SELECT '用量默认提醒','traffic_limit','all','[80,90,100]',id FROM users WHERE role='owner' ORDER BY id LIMIT 1;
INSERT INTO alert_rules(name,kind,scope_type,thresholds,created_by) SELECT '到期默认提醒','expiration','all','[7,3,1,0]',id FROM users WHERE role='owner' ORDER BY id LIMIT 1;
CREATE TABLE notification_preferences (
    user_id BIGINT PRIMARY KEY REFERENCES users(id), traffic_enabled BOOLEAN NOT NULL DEFAULT TRUE, expiration_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    revision BIGINT NOT NULL DEFAULT 1, updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE notification_records (
    id BIGSERIAL PRIMARY KEY, user_id BIGINT REFERENCES users(id), rule_id BIGINT REFERENCES alert_rules(id), kind TEXT NOT NULL, channel TEXT NOT NULL DEFAULT 'email',
    dedupe_key TEXT NOT NULL UNIQUE, event_snapshot JSONB NOT NULL, recipient_ciphertext BYTEA NOT NULL, payload_ciphertext BYTEA NOT NULL,
    email_revision BIGINT, state TEXT NOT NULL CHECK(state IN ('queued','sending','sent','retry','dead','suppressed','blocked_recipient')),
    attempts INT NOT NULL DEFAULT 0, next_attempt_at TIMESTAMPTZ NOT NULL, lease_until TIMESTAMPTZ, message_id TEXT NOT NULL UNIQUE,
    provider_status TEXT, last_error_code TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), sent_at TIMESTAMPTZ
);
CREATE INDEX notification_records_pending ON notification_records(state,next_attempt_at) WHERE state IN ('queued','retry','sending');
CREATE INDEX notification_records_user_created ON notification_records(user_id,created_at);
CREATE TABLE notification_attempts (
    id BIGSERIAL PRIMARY KEY, notification_id BIGINT NOT NULL REFERENCES notification_records(id), attempt_no INT NOT NULL,
    result TEXT NOT NULL, smtp_code INT, duration_ms INT NOT NULL, error_code TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE(notification_id,attempt_no)
);
CREATE TABLE email_verifications (
    id UUID PRIMARY KEY, purpose TEXT NOT NULL CHECK(purpose IN ('register','verify_existing')), user_id BIGINT REFERENCES users(id),
    email_ciphertext BYTEA NOT NULL, email_lookup_hash BYTEA NOT NULL, invite_lookup_hash BYTEA, token_hash BYTEA NOT NULL UNIQUE, ticket_hash BYTEA UNIQUE,
    email_revision BIGINT, state TEXT NOT NULL CHECK(state IN ('pending','verified','consumed','expired')),
    expires_at TIMESTAMPTZ NOT NULL, ticket_expires_at TIMESTAMPTZ, verified_at TIMESTAMPTZ, consumed_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX email_verifications_lookup ON email_verifications(email_lookup_hash,created_at);
