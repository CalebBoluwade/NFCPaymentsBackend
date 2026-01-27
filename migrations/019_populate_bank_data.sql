-- Update accounts with sample bank data
UPDATE accounts 
SET 
    bank_name = 'First Bank of Nigeria',
    bank_code = '011',
    is_primary = true
WHERE id IN (SELECT id FROM accounts LIMIT 1);

UPDATE accounts 
SET 
    bank_name = 'Access Bank',
    bank_code = '044'
WHERE bank_name IS NULL AND id IN (SELECT id FROM accounts WHERE bank_name IS NULL LIMIT 1);

UPDATE accounts 
SET 
    bank_name = 'Guaranty Trust Bank',
    bank_code = '058'
WHERE bank_name IS NULL AND id IN (SELECT id FROM accounts WHERE bank_name IS NULL LIMIT 1);

UPDATE accounts 
SET 
    bank_name = 'Zenith Bank',
    bank_code = '057'
WHERE bank_name IS NULL AND id IN (SELECT id FROM accounts WHERE bank_name IS NULL LIMIT 1);

UPDATE accounts 
SET 
    bank_name = 'United Bank for Africa',
    bank_code = '033'
WHERE bank_name IS NULL;
