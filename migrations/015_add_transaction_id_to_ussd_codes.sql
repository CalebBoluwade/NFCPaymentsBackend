-- Add transaction_id column to existing ussd_codes table
ALTER TABLE ussd_codes ADD COLUMN IF NOT EXISTS transaction_id VARCHAR(64);

-- Update existing rows with generated transaction IDs
UPDATE ussd_codes SET transaction_id = 'TXN-' || id || '-' || EXTRACT(EPOCH FROM created_at)::BIGINT WHERE transaction_id IS NULL;

-- Make transaction_id NOT NULL and UNIQUE
ALTER TABLE ussd_codes ALTER COLUMN transaction_id SET NOT NULL;
ALTER TABLE ussd_codes ADD CONSTRAINT ussd_codes_transaction_id_key UNIQUE (transaction_id);

-- Create index
CREATE INDEX IF NOT EXISTS idx_transaction_id ON ussd_codes(transaction_id);
