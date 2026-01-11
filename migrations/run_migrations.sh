#!/bin/bash

# Migration runner script for NFC Payments Backend
# This script runs all database migrations in the correct order

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

echo -e "${YELLOW}Starting database migrations...${NC}"

# Check if psql is available
if ! command -v psql &> /dev/null; then
    echo -e "${RED}Error: psql is not installed or not in PATH${NC}"
    exit 1
fi

# Test database connection
echo -e "${YELLOW}Testing database connection...${NC}"
export PGPASSWORD=$DB_PASSWORD
if ! psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -c "SELECT 1;" > /dev/null 2>&1; then
    echo -e "${RED}Error: Cannot connect to database${NC}"
    echo "Please check your database configuration and ensure the database is running"
    exit 1
fi

echo -e "${GREEN}Database connection successful${NC}"

# Create migrations table if it doesn't exist
echo -e "${YELLOW}Creating migrations tracking table...${NC}"
psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -c "
CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(255) PRIMARY KEY,
    applied_at TIMESTAMP NOT NULL DEFAULT NOW()
);" > /dev/null

# Function to run a migration
run_migration() {
    local migration_file=$1
    local version=$(basename "$migration_file" .sql)
    
    # Check if migration already applied
    local count=$(psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -t -c "SELECT COUNT(*) FROM schema_migrations WHERE version = '$version';" | xargs)
    
    if [ "$count" -eq "0" ]; then
        echo -e "${YELLOW}Running migration: $version${NC}"
        if psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -f "$migration_file" > /dev/null; then
            psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -c "INSERT INTO schema_migrations (version) VALUES ('$version');" > /dev/null
            echo -e "${GREEN}✓ Migration $version completed${NC}"
        else
            echo -e "${RED}✗ Migration $version failed${NC}"
            exit 1
        fi
    else
        echo -e "${GREEN}✓ Migration $version already applied${NC}"
    fi
}

# Get the directory of this script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Run migrations in order
for migration_file in "$SCRIPT_DIR"/*.sql; do
    if [ -f "$migration_file" ]; then
        run_migration "$migration_file"
    fi
done

echo -e "${GREEN}All migrations completed successfully!${NC}"

# Show migration status
echo -e "${YELLOW}Migration status:${NC}"
psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -c "SELECT version, applied_at FROM schema_migrations ORDER BY applied_at;"