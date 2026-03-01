DROP TABLE IF EXISTS delivery_metrics;
ALTER TABLE notifications DROP COLUMN IF EXISTS provider_msg_id;
ALTER TABLE channel_configs DROP COLUMN IF EXISTS delivery_prefs;
