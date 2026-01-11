-- Drop foreign key constraints to allow external account IDs
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_from_card_id_fkey;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_to_card_id_fkey;
