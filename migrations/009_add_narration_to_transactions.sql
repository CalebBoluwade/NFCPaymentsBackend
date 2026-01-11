-- Add narration column to transactions table if it doesn't exist
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS narration VARCHAR(200);
