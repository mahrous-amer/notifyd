CREATE TYPE channel_type AS ENUM ('discord', 'telegram', 'whatsapp');

CREATE TABLE channel_configs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel    channel_type NOT NULL,
    name       VARCHAR(255) NOT NULL,
    config     JSONB NOT NULL DEFAULT '{}',
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, channel, name)
);

CREATE INDEX idx_channel_configs_tenant_channel ON channel_configs(tenant_id, channel);
