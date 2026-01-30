#!/bin/sh
set -eu

# Create engram system group if not exists
if ! getent group engram >/dev/null 2>&1; then
    groupadd --system engram
fi

# Create engram system user if not exists
if ! getent passwd engram >/dev/null 2>&1; then
    useradd --system \
        --gid engram \
        --home-dir /var/lib/engram \
        --shell /usr/sbin/nologin \
        --comment "Engram Lore Service" \
        engram
fi

# Create data directory
mkdir -p /var/lib/engram
chown engram:engram /var/lib/engram
chmod 750 /var/lib/engram

# Create config directory
mkdir -p /etc/engram
