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
  id VARCHAR(36) PRIMARY KEY,
  doc_id VARCHAR(36) NOT NULL,
  text TEXT NOT NULL,
  section_title TEXT,
  source_url TEXT NOT NULL,
  chunk_index INTEGER NOT NULL,
  is_table BOOLEAN DEFAULT FALSE,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_parent_chunks_doc_id ON parent_chunks(doc_id);

-- Ingestion log (per-document, tracks latest status with upsert)
CREATE TABLE ingestion_log (
  id SERIAL PRIMARY KEY,
  doc_id VARCHAR(36) NOT NULL UNIQUE,
  source_url TEXT NOT NULL,
  chunk_count INT,
  content_hash VARCHAR(64) NOT NULL,
  status VARCHAR(20) NOT NULL,        -- success | failed | skipped
  error_message TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_ingestion_log_source_url ON ingestion_log(source_url);

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
  id SERIAL PRIMARY KEY,
  source_url TEXT NOT NULL,
  error_message TEXT NOT NULL,
  retry_count INTEGER DEFAULT 0,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  last_retry_at TIMESTAMPTZ
);
CREATE INDEX idx_dead_letter_source_url ON ingestion_dead_letter(source_url);

-- =============================================
-- DPO COPILOT TABLES
-- =============================================

-- DPO's client organizations
CREATE TABLE clients (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            VARCHAR(255) NOT NULL,
    description     TEXT,                          -- free-text business description
    sector          VARCHAR(100),                  -- "fintech", "healthtech", "saas", "ecommerce", etc.
    country         VARCHAR(2),                    -- ISO 3166-1 alpha-2
    employee_count  INTEGER,
    tech_stack      JSONB DEFAULT '[]'::jsonb,     -- ["stripe", "hubspot", "aws", "bamboohr"]
    data_subjects   JSONB DEFAULT '[]'::jsonb,     -- ["customers", "employees", "website_visitors"]
    processing_purposes JSONB DEFAULT '[]'::jsonb, -- ["email_marketing", "payment_processing", "hr"]
    status          VARCHAR(20) DEFAULT 'active',  -- active | archived
    created_at      TIMESTAMPTZ DEFAULT now(),
    updated_at      TIMESTAMPTZ DEFAULT now(),

    CONSTRAINT clients_user_name_unique UNIQUE (user_id, name)
);

CREATE INDEX idx_clients_user_id ON clients(user_id);
CREATE INDEX idx_clients_status ON clients(status);

-- Generated compliance artifacts
CREATE TABLE artifacts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id),
    type            VARCHAR(50) NOT NULL,          -- ropa | dpia_screening | dpa_gap | lawful_basis | ai_act_classification
    status          VARCHAR(20) DEFAULT 'draft',   -- draft | reviewed | approved | exported
    title           VARCHAR(500),
    input_context   TEXT NOT NULL,                  -- the business description that generated this
    generated_content JSONB NOT NULL,              -- structured artifact content (see schemas below)
    edited_content  JSONB,                         -- DPO's edited version (null until first edit)
    citations       JSONB DEFAULT '[]'::jsonb,     -- [{index, source_url, title, section, chunk_text}]
    generation_meta JSONB NOT NULL,                -- {provider, model, tokens_used, latency_ms, corpus_version}
    version         INTEGER DEFAULT 1,
    created_at      TIMESTAMPTZ DEFAULT now(),
    updated_at      TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_artifacts_client_id ON artifacts(client_id);
CREATE INDEX idx_artifacts_type ON artifacts(type);
CREATE INDEX idx_artifacts_status ON artifacts(status);
CREATE INDEX idx_artifacts_user_id ON artifacts(user_id);

-- Immutable audit trail for every artifact operation
CREATE TABLE artifact_audit_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id     UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id),
    action          VARCHAR(50) NOT NULL,          -- generated | edited | status_changed | exported | deleted
    previous_state  JSONB,                         -- snapshot before change
    new_state       JSONB,                         -- snapshot after change
    metadata        JSONB DEFAULT '{}'::jsonb,     -- {ip, user_agent, reason}
    created_at      TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_audit_artifact_id ON artifact_audit_log(artifact_id);
CREATE INDEX idx_audit_user_id ON artifact_audit_log(user_id);
CREATE INDEX idx_audit_created_at ON artifact_audit_log(created_at);

-- Audit log is append-only — no UPDATE or DELETE allowed
CREATE OR REPLACE FUNCTION prevent_audit_mutation() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'artifact_audit_log is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER no_audit_update BEFORE UPDATE ON artifact_audit_log
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_mutation();
CREATE TRIGGER no_audit_delete BEFORE DELETE ON artifact_audit_log
    FOR EACH ROW EXECUTE FUNCTION prevent_audit_mutation();

-- Common SaaS processor profiles
CREATE TABLE processor_profiles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(255) NOT NULL UNIQUE,   -- "Stripe", "HubSpot", "AWS"
    slug            VARCHAR(100) NOT NULL UNIQUE,    -- "stripe", "hubspot", "aws"
    category        VARCHAR(100),                    -- "payment", "crm", "cloud_infra", "hr", "analytics"
    description     TEXT,
    headquarters    VARCHAR(2),                      -- ISO country code
    data_categories JSONB DEFAULT '[]'::jsonb,       -- ["email", "name", "payment_card", "ip_address"]
    processing_purposes JSONB DEFAULT '[]'::jsonb,   -- ["payment_processing", "fraud_detection"]
    data_locations  JSONB DEFAULT '[]'::jsonb,       -- ["us", "eu", "global"]
    transfer_mechanism VARCHAR(50),                   -- "scc" | "dpf" | "adequacy" | "none_required"
    dpa_url         TEXT,                             -- link to their DPA if publicly available
    subprocessors_url TEXT,                           -- link to their subprocessor list
    gdpr_page_url   TEXT,                             -- link to their GDPR/privacy page
    verified        BOOLEAN DEFAULT false,
    last_verified   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT now(),
    updated_at      TIMESTAMPTZ DEFAULT now()
);

CREATE INDEX idx_processor_slug ON processor_profiles(slug);
CREATE INDEX idx_processor_category ON processor_profiles(category);

-- Artifact version history (for tracking DPO edits over time)
CREATE TABLE artifact_versions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id     UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    version         INTEGER NOT NULL,
    content         JSONB NOT NULL,
    edited_by       UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ DEFAULT now(),

    CONSTRAINT artifact_version_unique UNIQUE (artifact_id, version)
);
