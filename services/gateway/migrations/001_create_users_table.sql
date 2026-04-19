-- Create users table
CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(255) PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    full_name VARCHAR(255),
    plan VARCHAR(50) DEFAULT 'free' NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Create index on email for faster lookups
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);

-- Create index on plan for analytics
CREATE INDEX IF NOT EXISTS idx_users_plan ON users(plan);

-- Add constraint to validate plan values
ALTER TABLE users ADD CONSTRAINT check_plan_values
    CHECK (plan IN ('free', 'professional', 'team'));
