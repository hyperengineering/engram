#!/bin/sh
set -eu

# Set permissions on config files (only if they exist)
if [ -f /etc/engram/environment ]; then
    chmod 640 /etc/engram/environment
    chown root:engram /etc/engram/environment
fi

if [ -f /etc/engram/engram.yaml ]; then
    chmod 644 /etc/engram/engram.yaml
    chown root:engram /etc/engram/engram.yaml
fi

# Reload systemd only if available and running
if command -v systemctl >/dev/null 2>&1; then
    if systemctl is-system-running >/dev/null 2>&1 || [ "$(systemctl is-system-running 2>/dev/null)" = "offline" ]; then
        systemctl daemon-reload || true
    fi
fi

echo ""
echo "=========================================="
echo "  Engram has been installed successfully"
echo "=========================================="
echo ""
echo "Next steps:"
echo ""
echo "  1. Edit /etc/engram/environment to set API keys:"
echo "     - ENGRAM_API_KEY (for client authentication)"
echo "     - OPENAI_API_KEY (for embedding generation)"
echo ""
echo "  2. Review /etc/engram/engram.yaml for configuration"
echo ""
echo "  3. Start the service:"
echo "     systemctl start engram"
echo ""
echo "  4. Enable on boot:"
echo "     systemctl enable engram"
echo ""
echo "  5. Check status:"
echo "     systemctl status engram"
echo ""
