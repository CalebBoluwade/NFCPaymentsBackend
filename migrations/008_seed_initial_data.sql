-- Clear existing data
DELETE FROM accounts;

-- Alter table structure
-- ALTER TABLE accounts 
-- DROP COLUMN id CASCADE,
-- ADD COLUMN id SERIAL PRIMARY KEY,
-- ADD COLUMN account_name VARCHAR(255) NOT NULL,
-- ADD COLUMN account_id VARCHAR(50) UNIQUE NOT NULL,
-- ADD COLUMN card_id VARCHAR(50) UNIQUE;

-- Insert default system accounts
INSERT INTO accounts (account_name, account_id, balance, version, updated_at) VALUES 
('System Fees', '0000000001', 0, 1, NOW()),
('System Reserve', '0000000002', 0, 1, NOW());

-- Insert seed user accounts
INSERT INTO accounts (account_name, account_id, card_id, balance, version, updated_at) VALUES 
('John Doe', '1234567890', 'card_001', 15420, 1, NOW()),
('Jane Smith', '2345678901', 'card_002', 8750, 1, NOW()),
('Michael Johnson', '3456789012', 'card_003', 23100, 1, NOW()),
('Sarah Williams', '4567890123', 'card_004', 4680, 1, NOW()),
('David Brown', '5678901234', 'card_005', 19250, 1, NOW()),
('Emily Davis', '6789012345', 'card_006', 12340, 1, NOW()),
('James Wilson', '7890123456', 'card_007', 7890, 1, NOW()),
('Lisa Anderson', '8901234567', 'card_008', 16570, 1, NOW());

-- HSM keys will be generated automatically by the HSM service during initialization
-- No need to seed placeholder keys