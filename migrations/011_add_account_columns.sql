-- Add account_name and account_id columns to accounts table
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS account_name VARCHAR(255);
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS account_id VARCHAR(10) UNIQUE;
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS status VARCHAR(20) DEFAULT 'ACTIVE';

-- Create index for faster lookups
CREATE INDEX IF NOT EXISTS idx_accounts_account_id ON accounts(account_id);
