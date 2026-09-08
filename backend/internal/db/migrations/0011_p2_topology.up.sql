ALTER TABLE inbounds ADD COLUMN egress_mode TEXT NOT NULL DEFAULT 'direct', ADD COLUMN revision BIGINT NOT NULL DEFAULT 1;
UPDATE inbounds SET egress_mode='chain' WHERE role='relay' OR (role='entry' AND EXISTS (SELECT 1 FROM topology_drafts draft, jsonb_array_elements(draft.edges) edge WHERE (edge->>'fromInboundId')::bigint=inbounds.id));
ALTER TABLE inbounds ADD CONSTRAINT inbounds_egress_mode_check CHECK (egress_mode IN ('direct','chain') AND (role<>'relay' OR egress_mode='chain') AND (role<>'landing' OR egress_mode='direct'));
ALTER TABLE topology_drafts ADD COLUMN layout JSONB NOT NULL DEFAULT '{}', ADD COLUMN validation_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE topology_material_state ADD COLUMN graph_revision BIGINT NOT NULL DEFAULT 1;
ALTER TABLE topology_previews ADD COLUMN preview_id UUID UNIQUE, ADD COLUMN graph_revision BIGINT NOT NULL DEFAULT 1, ADD COLUMN revision_vector JSONB NOT NULL DEFAULT '{}', ADD COLUMN candidate_bundle JSONB NOT NULL DEFAULT '{}', ADD COLUMN expires_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE nodes ADD COLUMN agent_capabilities JSONB NOT NULL DEFAULT '[]', ADD COLUMN config_generation BIGINT NOT NULL DEFAULT 0;
CREATE TABLE topology_releases (
    id BIGSERIAL PRIMARY KEY, root_node_id BIGINT NOT NULL REFERENCES nodes(id),
    graph_revision BIGINT NOT NULL, material_revision BIGINT NOT NULL, revision_vector JSONB NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('preparing','ready','applying','succeeded','failed','rolling_back','rolled_back','manual_required')),
    created_by BIGINT NOT NULL REFERENCES users(id), error_code TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), finished_at TIMESTAMPTZ
);
CREATE INDEX topology_releases_active ON topology_releases(status,created_at) WHERE finished_at IS NULL;
CREATE TABLE topology_release_nodes (
    release_id BIGINT NOT NULL REFERENCES topology_releases(id), node_id BIGINT NOT NULL REFERENCES nodes(id),
    phase TEXT NOT NULL, candidate_topology JSONB NOT NULL, previous_topology JSONB, routing_version TEXT NOT NULL,
    expected_runtime_version TEXT, expected_generation BIGINT NOT NULL, previous_generation BIGINT NOT NULL,
    apply_order INT NOT NULL, status TEXT NOT NULL, attempts INT NOT NULL DEFAULT 0,
    last_ack_at TIMESTAMPTZ, error_code TEXT, PRIMARY KEY (release_id,node_id)
);
CREATE INDEX topology_release_nodes_node ON topology_release_nodes(node_id,release_id);
ALTER TABLE topology_deployments ADD COLUMN release_id BIGINT REFERENCES topology_releases(id), ADD COLUMN generation BIGINT NOT NULL DEFAULT 0;
