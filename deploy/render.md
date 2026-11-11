# Выкладка на Render (бесплатно) + база Neon

Сервис Render: `ohvat` → https://ohvat-jif6.onrender.com (ветка `claude/dazzling-newton-li353f`, автовыкладка при каждом пуше).
Образ: `deploy/render/Dockerfile` (nginx + Astro + Go в одном контейнере).

## База Neon — один раз
База создана: Neon, проект `ohvat` (Frankfurt), подключена к Render (DATABASE_URL в настройках сервиса). Ниже — как пересоздать вручную.

1. https://neon.tech → Sign up → Create project: имя `ohvat`, регион **Frankfurt (eu-central-1)**, Postgres 16.
2. На странице проекта → **Connect** → выключить «Connection pooling» → скопировать строку подключения
   (`postgresql://…neon.tech/neondb?sslmode=require`).
3. Render → сервис `ohvat` → **Environment** → Add Environment Variable:
   `DATABASE_URL` = эта строка → **Save, rebuild and deploy**.
   Строку никуда больше не пересылать (в чат, файлы, git — нельзя).
4. Через 3–5 минут сайт открывается; таблицы и данные главной создаются сами при запуске.

Бесплатный Render засыпает через 15 минут без посетителей — первое открытие после паузы идёт ~30–50 секунд. Чтобы
не засыпал, в render.yaml задан `KEEPALIVE_URL` (= `PUBLIC_URL`): бэкенд сам раз в 10 минут запрашивает `/healthz`
через публичный адрес. Сменили адрес сайта — поменяйте и `KEEPALIVE_URL`; на платном тарифе — оставьте пустым.
