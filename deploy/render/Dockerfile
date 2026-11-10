# Один образ для Render (бесплатный веб-сервис = один контейнер): nginx на $PORT → Astro (127.0.0.1:4321) и Go (127.0.0.1:8080).
# База — внешняя PostgreSQL по DATABASE_URL. Сборка из корня:
#   docker build --platform linux/amd64 -f deploy/render/Dockerfile -t ohvat-render .
# Локальный docker compose этот файл не трогает.
ARG REGISTRY=docker.io/library

FROM ${REGISTRY}/golang:1.25-alpine AS api
ENV CGO_ENABLED=0
WORKDIR /src
COPY backend/certs/ /tmp/certs/
RUN cat /tmp/certs/*.crt >> /etc/ssl/certs/ca-certificates.crt 2>/dev/null || true
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM ${REGISTRY}/node:22-alpine AS site
WORKDIR /app
COPY frontend/certs/ /tmp/certs/
RUN if ls /tmp/certs/*.crt >/dev/null 2>&1; then cat /tmp/certs/*.crt > /tmp/extra-ca.pem; fi
COPY frontend/package.json frontend/package-lock.json ./
RUN if [ -f /tmp/extra-ca.pem ]; then export NODE_EXTRA_CA_CERTS=/tmp/extra-ca.pem; fi; npm ci --no-audit --no-fund
COPY frontend/ .
RUN npm run build && npm prune --omit=dev

FROM ${REGISTRY}/node:22-alpine
COPY backend/certs/ /tmp/certs/
RUN cat /tmp/certs/*.crt >> /etc/ssl/certs/ca-certificates.crt 2>/dev/null || true; \
    apk add --no-cache nginx gettext tini && adduser -D -u 10001 app && rm -rf /tmp/certs
COPY --from=api /out/server /usr/local/bin/server
COPY --from=site /app/package.json /app/site/package.json
COPY --from=site /app/node_modules /app/site/node_modules
COPY --from=site /app/dist /app/site/dist
COPY deploy/render/nginx.conf.template /etc/ohvat/nginx.conf.template
COPY deploy/render/start.sh /usr/local/bin/start
RUN chmod +x /usr/local/bin/start
ENV PORT=10000 NODE_ENV=production HTTP_ADDR=127.0.0.1:8080 BACKEND_URL=http://127.0.0.1:8080
USER app
EXPOSE 10000
ENTRYPOINT ["/sbin/tini", "-g", "--", "start"]
