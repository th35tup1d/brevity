-- Users table for authentication and private links
CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Core URLs table (updated with user_id)
CREATE TABLE IF NOT EXISTS urls (
    id BIGSERIAL PRIMARY KEY,
    short_code VARCHAR(16) UNIQUE NOT NULL,
    long_url TEXT NOT NULL,
    user_id BIGINT REFERENCES users(id) ON DELETE CASCADE, -- NULL for anonymous, User ID for private links
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP WITH TIME ZONE
);

-- Crucial Index: Ensures O(1)-style index lookups when redirecting
CREATE INDEX IF NOT EXISTS idx_urls_short_code ON urls (short_code);

-- Index for querying user-specific links efficiently
CREATE INDEX IF NOT EXISTS idx_urls_user_id ON urls (user_id);

-- Click events table for asynchronous analytics
CREATE TABLE IF NOT EXISTS click_events (
    id BIGSERIAL PRIMARY KEY,
    url_id BIGINT NOT NULL REFERENCES urls(id) ON DELETE CASCADE,
    ip_address VARCHAR(45),
    user_agent TEXT,
    clicked_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Index for querying analytics per URL efficiently
CREATE INDEX IF NOT EXISTS idx_clicks_url_id ON click_events (url_id);
