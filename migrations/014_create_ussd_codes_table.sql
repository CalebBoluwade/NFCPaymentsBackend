-- Create USSD codes table for dynamic payment codes
CREATE TABLE IF NOT EXISTS ussd_codes (
    id BIGSERIAL PRIMARY KEY,
    transaction_id VARCHAR(64) NOT NULL UNIQUE,
    code_hash VARCHAR(64) NOT NULL UNIQUE,
    code_type VARCHAR(10) NOT NULL CHECK (code_type IN ('PUSH', 'PULL')),
    user_id VARCHAR(255) NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    expires_at TIMESTAMP NOT NULL,
    used BOOLEAN DEFAULT false,
    used_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes
CREATE INDEX idx_transaction_id ON ussd_codes(transaction_id);
CREATE INDEX idx_code_hash ON ussd_codes(code_hash);
CREATE INDEX idx_user_id ON ussd_codes(user_id);
CREATE INDEX idx_expires_at ON ussd_codes(expires_at);
CREATE INDEX idx_ussd_cleanup ON ussd_codes(expires_at, used, used_at);
