#!/bin/sh
set -eu

CONFIG_PATH="data/config.json"

mkdir -p "$(dirname "$CONFIG_PATH")"

if [ ! -f "$CONFIG_PATH" ]; then
  /app/zerobot-plugin -s "$CONFIG_PATH"
fi

exec /app/zerobot-plugin -c "$CONFIG_PATH" "$@"
