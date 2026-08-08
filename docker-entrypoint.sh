#!/bin/sh
set -e

DATA_DIR="${DATA_DIR:-/data}"
APP_USER="minitor"

mkdir -p "$DATA_DIR"
chown -R "$APP_USER:$APP_USER" "$DATA_DIR"

exec su-exec "$APP_USER" /minitor "$@"
