-- Fix data_types and third_party_processors columns to use TEXT[] instead of JSONB
-- This matches the Go code which uses pq.Array()

-- First drop the columns and recreate with correct type
-- (ALTER COLUMN ... TYPE doesn't work well for JSONB -> array conversion)

ALTER TABLE business_profiles
    DROP COLUMN IF EXISTS data_types,
    DROP COLUMN IF EXISTS third_party_processors;

ALTER TABLE business_profiles
    ADD COLUMN data_types TEXT[] DEFAULT '{}',
    ADD COLUMN third_party_processors TEXT[] DEFAULT '{}';
