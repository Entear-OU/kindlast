-- Kindlast PostgreSQL Schema
-- Run this as a K8s Job after StatefulSet is ready

-- Users and auth
CREATE TABLE users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT UNIQUE NOT NULL,
  email_hash TEXT NOT NULL,           -- sha256 for logging (never store plain in logs)
  plan TEXT NOT NULL DEFAULT 'free',  -- free | premium | api
  stripe_customer_id TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Subscriptions
CREATE TABLE subscriptions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES users(id) ON DELETE CASCADE,
  stripe_subscription_id TEXT UNIQUE,
  status TEXT NOT NULL,               -- active | cancelled | past_due
  current_period_end TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Parent chunks (large context for generation)
CREATE TABLE parent_chunks (
  id TEXT PRIMARY KEY,                -- deterministic UUID from doc_id + chunk_index
  doc_id TEXT NOT NULL,
  source_url TEXT NOT NULL,
  text TEXT NOT NULL,
  document_title TEXT,
  scraped_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_parent_chunks_doc_id ON parent_chunks(doc_id);

-- Ingestion log (per-document, per-run)
CREATE TABLE ingestion_log (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  doc_id TEXT NOT NULL,
  source_url TEXT NOT NULL,
  chunk_count INT,
  content_hash TEXT,
  status TEXT NOT NULL,               -- success | failed | skipped
  error_message TEXT,
  run_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_ingestion_log_doc_id ON ingestion_log(doc_id);
CREATE INDEX idx_ingestion_log_run_at ON ingestion_log(run_at);

-- Query audit log
CREATE TABLE query_log (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES users(id),
  query_hash TEXT NOT NULL,           -- sha256 of normalized query
  provider_used TEXT,                 -- claude | gpt-4o
  cache_hit BOOLEAN DEFAULT FALSE,
  chunk_count INT,
  latency_ms INT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- User feedback
CREATE TABLE response_feedback (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  query_hash TEXT NOT NULL,
  user_id UUID REFERENCES users(id),
  rating SMALLINT CHECK (rating IN (-1, 1)),
  comment TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Dead letter queue for failed ingestion
CREATE TABLE ingestion_dead_letter (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  source_url TEXT NOT NULL,
  failure_count INT DEFAULT 1,
  last_error TEXT,
  last_attempted_at TIMESTAMPTZ DEFAULT NOW(),
  resolved BOOLEAN DEFAULT FALSE
);
