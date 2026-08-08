#!/bin/sh
set -e

DATA_DIR="${DATA_DIR:-/data}"
APP_USER="minitor"

export DATA_DIR

mkdir -p "$DATA_DIR"

if [ "$(id -u)" = "0" ]; then
    chown -R "$APP_USER:$APP_USER" "$DATA_DIR"
    exec su-exec "$APP_USER" /minitor "$@"
fi

exec /minitor "$@"
