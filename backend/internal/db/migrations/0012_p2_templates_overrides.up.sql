ALTER TABLE templates ADD COLUMN description TEXT NOT NULL DEFAULT '', ADD COLUMN status TEXT NOT NULL DEFAULT 'draft', ADD COLUMN is_builtin BOOLEAN NOT NULL DEFAULT FALSE, ADD COLUMN published_version INT, ADD COLUMN revision BIGINT NOT NULL DEFAULT 1, ADD COLUMN created_by BIGINT REFERENCES users(id), ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), ADD COLUMN archived_at TIMESTAMPTZ;
CREATE TABLE template_versions (
    template_id BIGINT NOT NULL REFERENCES templates(id), version INT NOT NULL CHECK (version>0), schema_version INT NOT NULL,
    definition JSONB NOT NULL, checksum TEXT NOT NULL, published_by BIGINT NOT NULL REFERENCES users(id), published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (template_id,version)
);
ALTER TABLE templates ADD CONSTRAINT templates_published_version_fkey FOREIGN KEY (id,published_version) REFERENCES template_versions(template_id,version);
CREATE TABLE p2_template_reference_issues (
    subscription_id BIGINT PRIMARY KEY REFERENCES subscriptions(id), invalid_template_id BIGINT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), resolved_at TIMESTAMPTZ
);
INSERT INTO p2_template_reference_issues(subscription_id,invalid_template_id) SELECT id,template_id FROM subscriptions WHERE template_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM templates WHERE templates.id=subscriptions.template_id);
ALTER TABLE subscriptions ADD COLUMN template_version INT, ADD COLUMN revision BIGINT NOT NULL DEFAULT 1;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_template_id_fkey FOREIGN KEY (template_id) REFERENCES templates(id) NOT VALID;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_template_version_fkey FOREIGN KEY (template_id,template_version) REFERENCES template_versions(template_id,version) NOT VALID;
ALTER TABLE subscriptions ADD CONSTRAINT subscriptions_template_null_check CHECK (template_id IS NOT NULL OR template_version IS NULL);
ALTER TABLE node_groups ADD COLUMN subscription_defaults JSONB NOT NULL DEFAULT '{}', ADD COLUMN revision BIGINT NOT NULL DEFAULT 1;
ALTER TABLE subscription_node_overrides ALTER COLUMN sort_order DROP NOT NULL, ALTER COLUMN sort_order DROP DEFAULT, ADD COLUMN revision BIGINT NOT NULL DEFAULT 1;
ALTER TABLE subscription_inbound_overrides ALTER COLUMN sort_order DROP NOT NULL, ALTER COLUMN sort_order DROP DEFAULT, ADD COLUMN revision BIGINT NOT NULL DEFAULT 1;
ALTER TABLE inbounds ADD CONSTRAINT inbounds_node_id_id_unique UNIQUE(node_id,id);
ALTER TABLE subscription_inbound_overrides ADD CONSTRAINT subscription_inbound_binding_fkey FOREIGN KEY (node_id,inbound_id) REFERENCES inbounds(node_id,id) NOT VALID;
