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
-- fan-out lookup. The fan-out query (tenant_id + is_active + events @> ARRAY[...])
-- filters the array-containment check itself in SQL, not in application
-- code — this plain btree index narrows to the tenant's (at most 3) rows
-- first, and the @> check then runs over that tiny row set. A GIN index on
-- events, which is what "contains" queries normally want, would only pay
-- off filtering a large per-tenant row count; the per-tenant cap of 3
-- endpoints (enforced in the service layer) makes that payoff moot here.
CREATE INDEX idx_webhook_endpoints_tenant ON webhook_endpoints(tenant_id);
