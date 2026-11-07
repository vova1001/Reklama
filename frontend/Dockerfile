# Сайт (Astro, рендер на сервере). REGISTRY — откуда брать базовые образы.
ARG REGISTRY=docker.io/library
FROM ${REGISTRY}/node:22-alpine AS build
WORKDIR /app
# Необязательные доп. сертификаты (корпоративный прокси): положите *.crt в frontend/certs/.
COPY certs/ /tmp/certs/
RUN if ls /tmp/certs/*.crt >/dev/null 2>&1; then cat /tmp/certs/*.crt > /tmp/extra-ca.pem; fi
COPY package.json package-lock.json ./
RUN if [ -f /tmp/extra-ca.pem ]; then export NODE_EXTRA_CA_CERTS=/tmp/extra-ca.pem; fi; npm ci --no-audit --no-fund
COPY . .
RUN npm run build && npm prune --omit=dev

FROM ${REGISTRY}/node:22-alpine
WORKDIR /app
ENV NODE_ENV=production HOST=0.0.0.0 PORT=4321
COPY --from=build /app/package.json ./
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
USER node
EXPOSE 4321
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=5 CMD wget -qO- http://127.0.0.1:4321/robots.txt >/dev/null || exit 1
CMD ["node", "dist/server/entry.mjs"]
