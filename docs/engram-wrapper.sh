#!/bin/bash
# Engram wrapper script for macOS LaunchAgent
# Sources environment configuration before starting the service

set -e

# Get Homebrew prefix (handles both Apple Silicon and Intel)
if command -v brew &> /dev/null; then
    HOMEBREW_PREFIX="$(brew --prefix)"
else
    # Fallback if brew not in PATH during launchd execution
    if [[ -x "/opt/homebrew/bin/brew" ]]; then
        HOMEBREW_PREFIX="/opt/homebrew"
    elif [[ -x "/usr/local/bin/brew" ]]; then
        HOMEBREW_PREFIX="/usr/local"
    else
        echo "Error: Homebrew not found" >&2
        exit 1
    fi
fi

# Configuration file location
ENV_FILE="${HOMEBREW_PREFIX}/etc/engram/environment"

# Source environment variables if file exists
if [[ -f "$ENV_FILE" ]]; then
    set -a
    source "$ENV_FILE"
    set +a
else
    echo "Warning: Environment file not found at $ENV_FILE" >&2
    echo "Create it with ENGRAM_API_KEY and OPENAI_API_KEY" >&2
fi

# Engram binary location
ENGRAM_BIN="${HOMEBREW_PREFIX}/bin/engram"

if [[ ! -x "$ENGRAM_BIN" ]]; then
    echo "Error: engram binary not found at $ENGRAM_BIN" >&2
    exit 1
fi

exec "$ENGRAM_BIN" "$@"
