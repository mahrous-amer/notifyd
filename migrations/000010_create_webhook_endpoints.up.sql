CREATE TABLE webhook_endpoints (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    secret     TEXT NOT NULL,
    events     TEXT[] NOT NULL,
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Supports both the tenant-scoped list query and the dispatcher's per-event
-- fan-out lookup (tenant_id + is_active, filtered by events in application code
-- since Postgres cannot index "array contains" with a plain btree index).
CREATE INDEX idx_webhook_endpoints_tenant ON webhook_endpoints(tenant_id);
