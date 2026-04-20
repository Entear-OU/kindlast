-- Create business_profiles table for SME self-assessment flow
CREATE TABLE IF NOT EXISTS business_profiles (
    id VARCHAR(255) PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    company_name VARCHAR(255),
    country VARCHAR(100),
    industry VARCHAR(100),
    employee_count VARCHAR(50),
    processes_personal_data BOOLEAN DEFAULT false,
    data_types JSONB DEFAULT '[]'::jsonb,
    uses_ai_systems BOOLEAN DEFAULT false,
    ai_system_descriptions TEXT,
    third_party_processors JSONB DEFAULT '[]'::jsonb,
    transfers_data_outside_eu BOOLEAN DEFAULT false,
    has_dpo BOOLEAN DEFAULT false,
    has_privacy_policy BOOLEAN DEFAULT false,
    has_cookie_consent BOOLEAN DEFAULT false,
    has_breach_notification BOOLEAN DEFAULT false,
    has_dsr_process BOOLEAN DEFAULT false,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT business_profiles_user_id_unique UNIQUE (user_id)
);

CREATE INDEX IF NOT EXISTS idx_business_profiles_user_id ON business_profiles(user_id);

-- Create assessments table
CREATE TABLE IF NOT EXISTS assessments (
    id VARCHAR(255) PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    profile_id VARCHAR(255) REFERENCES business_profiles(id) ON DELETE SET NULL,
    type VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    overall_score INTEGER,
    risk_level VARCHAR(50),
    result JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT check_assessment_type CHECK (type IN ('gdpr', 'ai_act')),
    CONSTRAINT check_assessment_status CHECK (status IN ('pending', 'processing', 'complete', 'error'))
);

CREATE INDEX IF NOT EXISTS idx_assessments_user_id ON assessments(user_id);
CREATE INDEX IF NOT EXISTS idx_assessments_profile_id ON assessments(profile_id);
CREATE INDEX IF NOT EXISTS idx_assessments_created_at ON assessments(created_at DESC);

-- Create findings table
CREATE TABLE IF NOT EXISTS findings (
    id VARCHAR(255) PRIMARY KEY,
    assessment_id VARCHAR(255) NOT NULL REFERENCES assessments(id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category VARCHAR(100),
    severity VARCHAR(50),
    title TEXT NOT NULL,
    description TEXT,
    recommendation TEXT,
    gdpr_article VARCHAR(50),
    ai_act_article VARCHAR(50),
    is_resolved BOOLEAN DEFAULT false,
    resolved_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT check_finding_severity CHECK (severity IN ('critical', 'high', 'medium', 'low', 'info'))
);

CREATE INDEX IF NOT EXISTS idx_findings_assessment_id ON findings(assessment_id);
CREATE INDEX IF NOT EXISTS idx_findings_user_id ON findings(user_id);
CREATE INDEX IF NOT EXISTS idx_findings_is_resolved ON findings(is_resolved);
