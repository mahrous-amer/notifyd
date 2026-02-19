CREATE TYPE notification_status AS ENUM ('pending', 'processing', 'delivered', 'failed', 'retrying');

CREATE TABLE notifications (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_config_id UUID NOT NULL REFERENCES channel_configs(id) ON DELETE CASCADE,
    channel           channel_type NOT NULL,
    subject           VARCHAR(500),
    body              TEXT NOT NULL,
    metadata          JSONB DEFAULT '{}',
    status            notification_status NOT NULL DEFAULT 'pending',
    asynq_task_id     VARCHAR(255),
    retry_count       INT NOT NULL DEFAULT 0,
    max_retries       INT NOT NULL DEFAULT 5,
    last_error        TEXT,
    delivered_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notifications_tenant_created ON notifications(tenant_id, created_at DESC);
CREATE INDEX idx_notifications_tenant_status_created ON notifications(tenant_id, status, created_at DESC);
CREATE INDEX idx_notifications_asynq_task ON notifications(asynq_task_id);
