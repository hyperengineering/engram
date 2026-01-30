#!/bin/bash
set -e

echo "=== Post-start checks ==="

# Ensure Go modules are up to date
if [ ! -d "/go/pkg/mod/modernc.org" ]; then
    echo "Go module cache empty, downloading..."
    go mod download
fi

echo "=== Ready ==="
