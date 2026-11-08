#!/bin/sh
# Три процесса в одном контейнере. Упал любой — контейнер завершается, Render поднимает его заново.
set -eu
envsubst '${PORT}' < /etc/ohvat/nginx.conf.template > /tmp/nginx.conf
server &                                                        # API: миграции при старте, затем :8080
API=$!
PORT=4321 HOST=127.0.0.1 node /app/site/dist/server/entry.mjs &  # сайт
SITE=$!
nginx -e /dev/stderr -c /tmp/nginx.conf -g 'daemon off;' &                   # вход на $PORT
WEB=$!
while kill -0 "$API" 2>/dev/null && kill -0 "$SITE" 2>/dev/null && kill -0 "$WEB" 2>/dev/null; do sleep 2; done
echo "один из процессов остановился — перезапуск контейнера" >&2
exit 1
