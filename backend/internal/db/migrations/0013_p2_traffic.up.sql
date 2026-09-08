CREATE TABLE traffic_legacy_records AS SELECT *,now() AS quarantined_at FROM traffic_records;
DELETE FROM traffic_records;
ALTER TABLE traffic_records ADD COLUMN estimated BOOLEAN NOT NULL DEFAULT FALSE, ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), ADD CONSTRAINT traffic_records_nonnegative CHECK(up_bytes>=0 AND down_bytes>=0);
ALTER TABLE traffic_records DROP CONSTRAINT traffic_records_user_id_fkey, DROP CONSTRAINT traffic_records_node_id_fkey, DROP CONSTRAINT traffic_records_inbound_id_fkey;
ALTER TABLE traffic_records ADD FOREIGN KEY(user_id) REFERENCES users(id), ADD FOREIGN KEY(node_id) REFERENCES nodes(id), ADD FOREIGN KEY(inbound_id) REFERENCES inbounds(id);
CREATE TABLE traffic_reports (
    id BIGSERIAL PRIMARY KEY, node_id BIGINT NOT NULL REFERENCES nodes(id), epoch_id UUID NOT NULL, sequence BIGINT NOT NULL CHECK(sequence>0), payload_hash TEXT NOT NULL,
    period_start TIMESTAMPTZ NOT NULL, period_end TIMESTAMPTZ NOT NULL, received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(node_id,epoch_id,sequence), CHECK(period_end>period_start)
);
CREATE INDEX traffic_reports_received ON traffic_reports(received_at);
CREATE TABLE traffic_cursors (
    node_id BIGINT NOT NULL REFERENCES nodes(id), epoch_id UUID NOT NULL, series_key TEXT NOT NULL, last_sequence BIGINT NOT NULL,
    last_up_bytes BIGINT NOT NULL CHECK(last_up_bytes>=0), last_down_bytes BIGINT NOT NULL CHECK(last_down_bytes>=0), last_sample_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY(node_id,epoch_id,series_key)
);
CREATE TABLE traffic_node_state (
    node_id BIGINT PRIMARY KEY REFERENCES nodes(id), active_epoch_id UUID NOT NULL, last_sequence BIGINT NOT NULL,
    last_received_at TIMESTAMPTZ, last_complete_at TIMESTAMPTZ, coverage_start TIMESTAMPTZ,
    status TEXT NOT NULL CHECK(status IN ('collecting','stale','unsupported','gap'))
);
CREATE TABLE traffic_node_records (
    node_id BIGINT NOT NULL REFERENCES nodes(id), inbound_id BIGINT NOT NULL REFERENCES inbounds(id), period_start TIMESTAMPTZ NOT NULL,
    up_bytes BIGINT NOT NULL DEFAULT 0 CHECK(up_bytes>=0), down_bytes BIGINT NOT NULL DEFAULT 0 CHECK(down_bytes>=0),
    estimated BOOLEAN NOT NULL DEFAULT FALSE, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(node_id,inbound_id,period_start)
);
CREATE TABLE traffic_user_aggregates (
    user_id BIGINT NOT NULL REFERENCES users(id), node_id BIGINT NOT NULL REFERENCES nodes(id), grain TEXT NOT NULL CHECK(grain IN ('1h','1d')), period_start TIMESTAMPTZ NOT NULL,
    up_bytes BIGINT NOT NULL DEFAULT 0 CHECK(up_bytes>=0), down_bytes BIGINT NOT NULL DEFAULT 0 CHECK(down_bytes>=0),
    estimated BOOLEAN NOT NULL DEFAULT FALSE, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(user_id,node_id,grain,period_start)
);
CREATE INDEX traffic_user_aggregates_period ON traffic_user_aggregates(user_id,grain,period_start);
CREATE TABLE traffic_node_aggregates (
    node_id BIGINT NOT NULL REFERENCES nodes(id), grain TEXT NOT NULL CHECK(grain IN ('1h','1d')), period_start TIMESTAMPTZ NOT NULL,
    up_bytes BIGINT NOT NULL DEFAULT 0 CHECK(up_bytes>=0), down_bytes BIGINT NOT NULL DEFAULT 0 CHECK(down_bytes>=0),
    estimated BOOLEAN NOT NULL DEFAULT FALSE, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(node_id,grain,period_start)
);
CREATE TABLE traffic_accounts (
    user_id BIGINT PRIMARY KEY REFERENCES users(id), quota_epoch BIGINT NOT NULL DEFAULT 1, period_start TIMESTAMPTZ NOT NULL,
    used_up_bytes BIGINT NOT NULL DEFAULT 0 CHECK(used_up_bytes>=0), used_down_bytes BIGINT NOT NULL DEFAULT 0 CHECK(used_down_bytes>=0),
    lifetime_up_bytes BIGINT NOT NULL DEFAULT 0 CHECK(lifetime_up_bytes>=0), lifetime_down_bytes BIGINT NOT NULL DEFAULT 0 CHECK(lifetime_down_bytes>=0), updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO traffic_accounts(user_id,period_start) SELECT id,now() FROM users;
CREATE TABLE traffic_account_events (
    id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL REFERENCES users(id), quota_epoch BIGINT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('limit_changed','reset')),
    old_limit_bytes BIGINT NOT NULL, new_limit_bytes BIGINT NOT NULL, snapshot_up_bytes BIGINT NOT NULL, snapshot_down_bytes BIGINT NOT NULL,
    actor_id BIGINT NOT NULL REFERENCES users(id), created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE traffic_rollup_jobs (
    grain TEXT NOT NULL CHECK(grain IN ('1h','1d')), period_start TIMESTAMPTZ NOT NULL, revision BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(grain,period_start)
);
