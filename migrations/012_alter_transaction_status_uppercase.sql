-- Alter transaction status constraint to accept uppercase values
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_status_check;
ALTER TABLE transactions ADD CONSTRAINT transactions_status_check 
    CHECK (status IN ('PENDING', 'PROCESSING', 'COMPLETED', 'FAILED', 'CANCELLED', 'FAILED_ACCOUNT_NOT_FOUND', 'FAILED_ACCOUNT_NOT_ACTIVE', 'FAILED_INSUFFICIENT_BALANCE', 'FAILED_DEBIT_ERROR', 'FAILED_ISO_CONVERSION', 'FAILED_SETTLEMENT_ERROR'));
