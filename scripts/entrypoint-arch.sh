#!/bin/bash

# Arch Linux specific entrypoint script for Echo VR Server with MATE DE support

# Start VNC server if enabled
if [ "$VNC_ENABLED" = "true" ]; then
    echo "Starting VNC server..."
    if command -v vncserver &> /dev/null; then
        vncserver :1 -geometry 1920x1080 -depth 24 &
        export DISPLAY=:1
    else
        echo "VNC server not available, skipping..."
    fi
fi

# Start MATE desktop session if X11 is available and we're not in headless mode
if [ -n "$DISPLAY" ] && command -v mate-session &> /dev/null; then
    echo "MATE Desktop Environment available"
    # Start mate-session in background for GUI apps
    mate-session &
fi

# Export wine environment variables
export WINEARCH=win64
export WINEPREFIX=/home/container/.wine
export WINEDEBUG=-all

# Ensure wine prefix exists
if [ ! -d "$WINEPREFIX" ]; then
    echo "Initializing Wine prefix..."
    wine wineboot --init
fi

# Execute the original entrypoint script
exec bash /scripts/entrypoint.sh "$@"