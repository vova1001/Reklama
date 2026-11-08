# Выкладка на Render (бесплатно) + база Neon

Сервис Render: `ohvat` → https://ohvat-jif6.onrender.com (ветка `claude/dazzling-newton-li353f`, автовыкладка при каждом пуше).
Образ: `deploy/render/Dockerfile` (nginx + Astro + Go в одном контейнере).

## База Neon — один раз
1. https://neon.tech → Sign up (удобно через GitHub) → Create project: имя `ohvat`, регион **Frankfurt (eu-central-1)**, Postgres 16.
2. На странице проекта → **Connect** → выключить «Connection pooling» → скопировать строку подключения
   (`postgresql://…neon.tech/neondb?sslmode=require`).
3. Render → сервис `ohvat` → **Environment** → Add Environment Variable:
   `DATABASE_URL` = эта строка → **Save, rebuild and deploy**.
   Строку никуда больше не пересылать (в чат, файлы, git — нельзя).
4. Через 3–5 минут сайт открывается; таблицы и данные главной создаются сами при запуске.

Бесплатный Render засыпает через 15 минут без посетителей — первое открытие после паузы идёт ~30–50 секунд.
