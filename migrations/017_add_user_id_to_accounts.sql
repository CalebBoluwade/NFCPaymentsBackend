-- Add user_id column to accounts table for better ownership tracking
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS user_id INTEGER REFERENCES users(id);

-- Create index for performance
CREATE INDEX IF NOT EXISTS idx_accounts_user_id ON accounts(user_id);

-- Backfill user_id from cards table
UPDATE accounts a
SET user_id = c.user_id
FROM cards c
WHERE a.card_id = c.card_id AND a.user_id IS NULL;
