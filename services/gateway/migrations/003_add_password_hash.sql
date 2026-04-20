-- Add password_hash column to users table for authentication
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT;

-- Add full_name column if missing
ALTER TABLE users ADD COLUMN IF NOT EXISTS full_name VARCHAR(255);
