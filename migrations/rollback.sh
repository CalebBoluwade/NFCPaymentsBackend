#!/bin/bash

# Rollback script for NFC Payments Backend migrations
# WARNING: This will drop all tables and data - use only in development!

set -e

# Database configuration
DB_HOST=${DATABASE_HOST:-localhost}
DB_PORT=${DATABASE_PORT:-5432}
DB_USER=${DATABASE_USER:-postgres}
DB_PASSWORD=${DATABASE_PASSWORD:-password}
DB_NAME=${DATABASE_NAME:-nfc_payments}

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${RED}WARNING: This will drop all tables and data!${NC}"
echo -e "${YELLOW}Are you sure you want to continue? (y/N)${NC}"
read -r response

if [[ ! "$response" =~ ^[Yy]$ ]]; then
    echo "Rollback cancelled"
    exit 0
fi

export PGPASSWORD=$DB_PASSWORD

echo -e "${YELLOW}Rolling back all migrations...${NC}"

# Drop tables in reverse dependency order
psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME << EOF
-- Drop triggers first
DROP TRIGGER IF EXISTS update_users_updated_at ON users;
DROP TRIGGER IF EXISTS update_cards_updated_at ON cards;
DROP TRIGGER IF EXISTS update_transactions_updated_at ON transactions;
DROP TRIGGER IF EXISTS update_accounts_updated_at ON accounts;

-- Drop function
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS audit_events CASCADE;
DROP TABLE IF EXISTS hsm_keys CASCADE;
DROP TABLE IF EXISTS transactions CASCADE;
DROP TABLE IF EXISTS cards CASCADE;
DROP TABLE IF EXISTS users CASCADE;
DROP TABLE IF EXISTS ledger_entries CASCADE;
DROP TABLE IF EXISTS accounts CASCADE;
DROP TABLE IF EXISTS schema_migrations CASCADE;
EOF

echo -e "${GREEN}All tables dropped successfully${NC}"
echo -e "${YELLOW}You can now run migrations again with ./run_migrations.sh${NC}"