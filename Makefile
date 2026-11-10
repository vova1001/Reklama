# «Охват» — команды разработки. Всё запускается в Docker.
SHELL := /bin/sh
COMPOSE := docker compose

.PHONY: env dev up down logs test e2e screens lint gen prod

## .env из .env.example со случайным паролем базы и AUTH_SECRET (если .env ещё нет)
env:
	@if [ ! -f .env ]; then \
	  cp .env.example .env; \
	  sed -i "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$$(head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 20)/" .env; \
	  sed -i "s/^AUTH_SECRET=.*/AUTH_SECRET=$$(head -c 64 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 48)/" .env; \
	  echo ".env создан"; \
	else echo ".env уже есть"; fi

## поднять всё: http://localhost:8000
dev: env
	$(COMPOSE) up --build -d --wait
	@echo "Сайт: http://localhost:$${WEB_PORT:-8000}"

up: dev

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f backend frontend

## тесты бэка: модульные + интеграционные на PostgreSQL в Docker
test:
	$(COMPOSE) --profile test run --rm --build backend-test; s=$$?; $(COMPOSE) --profile test stop db-test >/dev/null 2>&1; exit $$s

## e2e (Playwright) против поднятого стека (make dev)
e2e:
	cd frontend && npx playwright test

## скриншоты 375 / 768 / 1440 → project/screens/ (главная и экраны входа)
screens:
	cd frontend && node scripts/screens.mjs && node scripts/screens-auth.mjs

## свой сервер: https через Caddy (DOMAIN и PUBLIC_URL в .env) — deploy/README.md
prod:
	$(COMPOSE) -f docker-compose.prod.yml up --build -d --wait

## проверки кода
lint:
	cd backend && test -z "$$(gofmt -l .)" && go vet ./...
	cd frontend && npx astro check

## код store из SQL (sqlc в Docker) и типы фронта из OpenAPI
gen:
	docker run --rm -v $(CURDIR)/backend:/src -w /src sqlc/sqlc:1.29.0 generate
	cd frontend && npm run gen:api
