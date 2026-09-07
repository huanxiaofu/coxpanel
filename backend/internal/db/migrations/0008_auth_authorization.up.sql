-- 0008 auth_authorization
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;

CREATE TABLE IF NOT EXISTS user_node_groups (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id   BIGINT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_user_node_groups_group ON user_node_groups (group_id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'invite_codes_node_group_id_fkey'
    ) THEN
        ALTER TABLE invite_codes
            ADD CONSTRAINT invite_codes_node_group_id_fkey
            FOREIGN KEY (node_group_id) REFERENCES node_groups(id) ON DELETE SET NULL;
    END IF;
END $$;
