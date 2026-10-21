#!/bin/bash
# Wings entrypoint: runs the egg's startup command (normally evr-supervisor).
# The installer container gets the install command as arguments: run it as is.
if [ "$#" -gt 0 ]; then
  exec "$@"
fi
cd /home/container || exit 1

if [ "${GAME_SOURCE:-mount}" = "download" ]; then
  export EVR_GAME_DIR="${EVR_GAME_DIR:-/home/container/echovr/game/current}"
  export EVR_NEVR_DIR="${EVR_NEVR_DIR:-/home/container/echovr/nevr}"
else
  export EVR_GAME_DIR="${EVR_GAME_DIR:-/opt/echovr/game/current}"
  export EVR_NEVR_DIR="${EVR_NEVR_DIR:-/opt/echovr/nevr}"
fi
export EVR_PORT="${EVR_PORT:-$SERVER_PORT}"
export EVR_INSTANCE="${EVR_INSTANCE:-${P_SERVER_UUID:-$HOSTNAME}}"
export EVR_HOME=/home/container HOME=/home/container USER=container

# The panel passes the startup command with {{VARIABLE}} placeholders.
STARTUP_CMD=$(echo "${STARTUP:-evr-supervisor}" | sed -e 's/{{/${/g' -e 's/}}/}/g')
echo "[evr] starting: ${STARTUP_CMD}"
eval "exec ${STARTUP_CMD}"
