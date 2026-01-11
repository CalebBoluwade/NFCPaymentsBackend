#!/bin/bash

# Test runner script for NFC Payments Backend services

echo "Running unit tests for all services..."
echo "======================================"

# Set test environment variables
export GO_ENV=test

# Run tests with coverage
go test -v -race -coverprofile=coverage.out ./internal/services/...

# Generate coverage report
if [ -f coverage.out ]; then
    echo ""
    echo "Coverage Summary:"
    echo "=================="
    go tool cover -func=coverage.out | tail -1
    
    # Generate HTML coverage report
    go tool cover -html=coverage.out -o coverage.html
    echo "HTML coverage report generated: coverage.html"
fi

echo ""
echo "Test run completed!"