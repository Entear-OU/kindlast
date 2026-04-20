-- Create users table
-- Note: This migration is kept for reference but the table may already exist
-- with a slightly different schema (e.g., UUID id, email_hash column)
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT UNIQUE NOT NULL,
    email_hash TEXT NOT NULL DEFAULT '',
    password_hash TEXT,
    full_name VARCHAR(255),
    plan TEXT DEFAULT 'free' NOT NULL,
    stripe_customer_id TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Create index on email for faster lookups
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);

-- Create index on plan for analytics
CREATE INDEX IF NOT EXISTS idx_users_plan ON users(plan);

-- Add constraint to validate plan values (use DO block to handle if exists)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'check_plan_values'
    ) THEN
        ALTER TABLE users ADD CONSTRAINT check_plan_values
            CHECK (plan IN ('free', 'professional', 'team'));
    END IF;
END $$;
