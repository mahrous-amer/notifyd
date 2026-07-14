-- See 000008's comment: ALTER TYPE ... ADD VALUE must be the only statement
-- in its migration file, so 'webhook' gets its own migration after 'slack'.
ALTER TYPE channel_type ADD VALUE 'webhook';
