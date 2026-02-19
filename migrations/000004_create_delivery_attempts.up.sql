CREATE TYPE attempt_status AS ENUM ('success', 'failure');

CREATE TABLE delivery_attempts (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id   UUID NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    attempt_number    INT NOT NULL,
    status            attempt_status NOT NULL,
    provider_response JSONB,
    error_message     TEXT,
    duration_ms       INT,
    attempted_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_delivery_attempts_notification ON delivery_attempts(notification_id);
CREATE INDEX idx_delivery_attempts_notification_attempt ON delivery_attempts(notification_id, attempt_number);
