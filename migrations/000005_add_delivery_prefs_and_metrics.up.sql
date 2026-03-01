ALTER TABLE channel_configs ADD COLUMN delivery_prefs JSONB DEFAULT '{}';
ALTER TABLE notifications ADD COLUMN provider_msg_id VARCHAR(255);

CREATE TABLE delivery_metrics (
    id               UUID PRIMARY KEY,
    notification_id  UUID NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    provider_msg_id  VARCHAR(255) NOT NULL,
    delivered_at     TIMESTAMPTZ,
    read_at          TIMESTAMPTZ,
    interactions     JSONB DEFAULT '{}',
    collected_at     TIMESTAMPTZ NOT NULL,
    UNIQUE(notification_id)
);

CREATE INDEX idx_notifications_provider_msg ON notifications(provider_msg_id);
