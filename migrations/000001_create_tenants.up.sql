CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE tenants (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(255) NOT NULL,
    slug       VARCHAR(100) NOT NULL UNIQUE,
    api_key    VARCHAR(64) NOT NULL UNIQUE,
    api_secret TEXT NOT NULL,
    is_active  BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- UNIQUE constraints already create implicit indexes on slug and api_key
