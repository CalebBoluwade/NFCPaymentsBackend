ALTER TABLE accounts ADD COLUMN IF NOT EXISTS is_primary BOOLEAN DEFAULT false;
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS bank_name VARCHAR(255);
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS bank_code VARCHAR(20);

CREATE INDEX IF NOT EXISTS idx_accounts_is_primary ON accounts(is_primary);
CREATE INDEX IF NOT EXISTS idx_accounts_bank_code ON accounts(bank_code);
