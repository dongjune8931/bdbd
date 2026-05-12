-- 000001_init.up.sql
-- Initial schema for BodyBuddy.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- users: authentication and profile data.
CREATE TABLE users (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    email         VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- characters: gamified character state linked to a user (1-to-1).
CREATE TABLE characters (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID         UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         VARCHAR(100) NOT NULL DEFAULT 'My Character',
    level        INT          NOT NULL DEFAULT 1,
    total_score  INT          NOT NULL DEFAULT 0,
    last_updated TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- score_history: immutable audit log of every score update.
-- upload_id UNIQUE ensures SQS redeliveries don't produce duplicate rows.
CREATE TABLE score_history (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    upload_id        UUID        NOT NULL UNIQUE,
    score            INT         NOT NULL,
    score_breakdown  JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_score_history_user_id ON score_history(user_id);

-- inbody_uploads: tracks each S3 upload through the analysis pipeline.
-- idempotency_key stores the SQS message ID to prevent duplicate processing.
CREATE TABLE inbody_uploads (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    s3_key           VARCHAR(500) NOT NULL,
    status           VARCHAR(50)  NOT NULL DEFAULT 'pending',
    idempotency_key  VARCHAR(255) UNIQUE,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    processed_at     TIMESTAMPTZ
);

CREATE INDEX idx_inbody_uploads_user_id ON inbody_uploads(user_id);
CREATE INDEX idx_inbody_uploads_status  ON inbody_uploads(status);
