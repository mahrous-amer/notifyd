CREATE TABLE tenant_entitlements (
    tenant_id        UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    plan_code        VARCHAR(50)  NOT NULL,
    message_limit    BIGINT       NOT NULL,
    allowed_channels TEXT[]       NOT NULL,
    api_key_limit    INT          NOT NULL DEFAULT 1,
    retention_days   INT          NOT NULL DEFAULT 7,
    period_start     TIMESTAMPTZ  NOT NULL,
    period_end       TIMESTAMPTZ  NOT NULL,
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE api_keys (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    api_key         VARCHAR(64) NOT NULL UNIQUE,
    api_secret_hash TEXT NOT NULL,
    label           VARCHAR(100) NOT NULL DEFAULT 'default',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX idx_api_keys_tenant ON api_keys(tenant_id);

-- Backfill: every existing tenant's key pair becomes its first api_keys row.
INSERT INTO api_keys (tenant_id, api_key, api_secret_hash, label)
SELECT id, api_key, api_secret, 'default' FROM tenants;
