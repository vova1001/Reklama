import { test, expect, type Page } from '@playwright/test';
import { execSync } from 'node:child_process';

// Вход по почте: код берётся из журнала бэкенда (режим разработки, MAIL_PROVIDER=log) — сайт поднят через docker compose.
const repo = new URL('../../../', import.meta.url).pathname;

function codeFor(email: string): string {
  const logs = execSync('docker compose logs backend --no-log-prefix --since 10m', { cwd: repo, encoding: 'utf8', maxBuffer: 32 << 20 });
  const line = logs.split('\n').filter((l) => l.includes(`"to":"${email}"`) && l.includes('Код для входа')).at(-1);
  const m = line?.match(/Код для входа в Охват: (\d{6})/);
  if (!m) throw new Error(`нет кода в журнале для ${email}`);
  return m[1];
}

const uniq = (p: string, name: string) => `e2e-${name}-${p}-${Date.now()}-${Math.floor(Math.random() * 1e6)}@example.com`;

// ошибки JS на странице — недопустимы (ответы 4xx здесь ожидаемы: неверный код, лимиты)
function jsErrors(page: Page) {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  return errors;
}

test.beforeEach(async ({ context }) => {
  await context.route('https://telegram.org/**', (r) => r.abort()); // окно Telegram в тестах не открываем
});

async function requestCode(page: Page, email: string) {
  await page.goto('/login');
  await page.getByRole('button', { name: 'Войти по почте' }).click();
  await page.getByLabel('Почта').fill(email);
  await page.getByRole('button', { name: 'Получить код' }).click();
  await expect(page.getByRole('heading', { name: 'Введите код' })).toBeVisible();
}

test('страница входа: одна карточка без выбора роли, способы входа, согласие строкой, без прокрутки вбок', async ({ page }) => {
  const errors = jsErrors(page);
  const res = await page.goto('/login');
  expect(res?.status()).toBe(200);
  await expect(page.locator('h1')).toHaveText('Вход в Охват');
  // минимализм: без шапки сайта и колонки преимуществ, знак ведёт на главную
  await expect(page.locator('.bar')).toHaveCount(0);
  await expect(page.locator('.au-mark')).toHaveAttribute('href', '/');
  const card = await page.locator('.au-card').boundingBox();
  expect(card!.width).toBeLessThanOrEqual(420);
  // роль не выбирается: один аккаунт — два режима
  await expect(page.getByRole('radio')).toHaveCount(0);
  await expect(page.locator('.au-sub')).toContainText('для бизнеса и для автора');
  // способы входа — пилюли одна под другой: Telegram, Google, почта
  const cfg = await (await page.request.get('/api/auth/config')).json();
  const ways = page.locator('.ways > .way');
  const names = ['Войти через Telegram', 'Войти через Google', 'Войти по почте'].filter((_, i) => [!!cfg.telegram, cfg.google, cfg.email][i]);
  await expect(ways).toHaveText(names);
  const boxes = await Promise.all((await ways.all()).map((w) => w.boundingBox()));
  for (let i = 1; i < boxes.length; i++) expect(boxes[i]!.y).toBeGreaterThan(boxes[i - 1]!.y + boxes[i - 1]!.height - 1);
  // согласие — одна строка мелко (не меньше 14px), без чекбокса
  await expect(page.getByRole('checkbox')).toHaveCount(0);
  const legal = page.locator('.au-legal');
  await expect(legal).toContainText('Продолжая, вы принимаете');
  expect(parseFloat(await legal.evaluate((e) => getComputedStyle(e).fontSize))).toBeGreaterThanOrEqual(14);
  for (const w of [320, 360, 375, 768, 1440]) {
    await page.setViewportSize({ width: w, height: 740 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(w);
  }
  expect(errors).toEqual([]);
});

test('ошибки формы почты: пустая, неверный адрес, нет связи; назад к способам', async ({ page }) => {
  await page.goto('/login');
  await page.getByRole('button', { name: 'Войти по почте' }).click();
  await expect(page.getByLabel('Почта')).toBeFocused();
  const submit = page.getByRole('button', { name: 'Получить код' });
  await submit.click();
  await expect(page.locator('[data-err="email"]')).toHaveText('Введите почту');
  await expect(page.getByLabel('Почта')).toHaveAttribute('aria-invalid', 'true');
  await page.getByLabel('Почта').fill('anna@');
  await submit.click();
  await expect(page.locator('[data-err="email"]')).toContainText('Проверьте адрес почты');
  await page.getByLabel('Почта').fill('anna@mail.ru');
  await page.route('**/api/auth/email/start', (r) => r.abort());
  await submit.click();
  await expect(page.locator('[data-pane="email"] [data-err="form"]')).toContainText('Нет связи с сервером');
  await expect(submit).toBeEnabled();
  await page.getByRole('button', { name: 'Назад' }).click();
  await expect(page.locator('h1')).toBeVisible();
});

test('вход по почте → сразу приложение → выход', async ({ page }, info) => {
  const errors = jsErrors(page);
  const email = uniq(info.project.name, 'mail');
  await requestCode(page, email);
  await expect(page.locator('[data-sent-to]')).toHaveText(email);
  await expect(page.locator('[data-resend-wait]')).toContainText('Отправить ещё раз можно через');

  const code = codeFor(email);
  const wrong = code === '000000' ? '111111' : '000000';
  // неверный код (вставка из буфера тоже работает — fill в первую ячейку раскладывает цифры)
  await page.locator('[data-digit]').first().fill(wrong);
  await expect(page.locator('[data-err="code"]')).toHaveText('Неверный код. Осталось попыток: 4.');
  await expect(page.locator('[data-digit]').first()).toHaveAttribute('aria-invalid', 'true');
  // неполный код
  await page.locator('[data-digit]').first().fill('12');
  await page.getByRole('button', { name: 'Войти' }).click();
  await expect(page.locator('[data-err="code"]')).toHaveText('Введите все 6 цифр из письма');
  // верный — автопереход сразу в приложение, без анкеты
  await page.locator('[data-digit]').first().fill(code);
  await page.waitForURL('**/app');
  await expect(page.locator('[data-app]')).toHaveAttribute('data-mode', 'business');
  await expect(page.getByRole('heading', { level: 1, name: 'Авторы' })).toBeVisible();

  // /login и /account для вошедшего — в приложение
  await page.goto('/login');
  await expect(page).toHaveURL(/\/app$/);
  await page.goto('/account');
  await expect(page).toHaveURL(/\/app$/);
  // на главной вместо «Войти» — аватар со ссылкой в приложение
  await page.goto('/');
  await expect(page.locator('[data-me]')).toHaveAttribute('href', '/app');
  await expect(page.locator('.bar .login')).toHaveCount(0);

  // cookie сессии — httpOnly, SameSite=Lax
  const sess = (await page.context().cookies()).find((c) => c.name === 'ohvat_session');
  expect(sess?.httpOnly).toBe(true);
  expect(sess?.sameSite).toBe('Lax');

  await page.goto('/app');
  await page.locator('[data-menu-btn]:visible').first().click();
  await page.getByRole('menuitem', { name: 'Выйти' }).click();
  await page.waitForURL((u) => u.pathname === '/');
  await expect(page.locator('.bar .login')).toHaveCount(1);
  await page.goto('/app');
  await expect(page).toHaveURL(/\/login$/);
  expect(errors).toEqual([]);
});

test('повторная отправка кода: таймер 60 с, лимит 429 с обратным отсчётом', async ({ page }, info) => {
  const email = uniq(info.project.name, 'limit');
  await requestCode(page, email);
  await expect(page.locator('[data-resend]')).toBeHidden();
  // назад и сразу ещё раз — сервер отвечает 429, кнопка показывает таймер
  await page.getByRole('button', { name: 'Другая почта' }).click();
  await page.getByRole('button', { name: 'Получить код' }).click();
  await expect(page.locator('[data-pane="email"] [data-err="form"]')).toContainText('Новый код можно запросить через');
  await expect(page.locator('[data-submit]')).toBeDisabled();
  await expect(page.locator('[data-submit]')).toContainText(/Повторить через 0:\d\d/);
});

test('ошибки входа через Google из адреса показываются понятным текстом', async ({ page }) => {
  await page.goto('/login?error=google_state');
  await expect(page.locator('[data-flash]')).toContainText('Вход через Google прервался');
  await expect(page).toHaveURL(/\/login$/);
});

test('без CSRF-токена изменяющий запрос отклоняется', async ({ request }) => {
  const res = await request.post('/api/auth/email/start', { data: { email: 'a@mail.ru', consent: true } });
  expect(res.status()).toBe(403);
  expect((await res.json()).error.code).toBe('csrf');
});

test('Google: переход с state и PKCE; возврат с чужим state или отменой — понятная ошибка', async ({ page, request }) => {
  const cfg = await (await request.get('/api/auth/config')).json();
  test.skip(!cfg.google, 'ключи Google не заданы');
  const r = await request.get('/api/auth/google/start?consent=1', { maxRedirects: 0 });
  expect(r.status()).toBe(302);
  const loc = new URL(r.headers()['location']);
  expect(loc.host).toBe('accounts.google.com');
  expect(loc.searchParams.get('code_challenge_method')).toBe('S256');
  expect(loc.searchParams.get('state')).toBeTruthy();
  expect(r.headers()['set-cookie']).toMatch(/ohvat_oauth=.*HttpOnly/i);
  // эмуляция возврата из Google (обмен кода не вызывается — state не совпал / человек отменил)
  await page.goto('/api/auth/google/callback?state=forged&code=x');
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.locator('[data-flash]')).toContainText('Вход через Google прервался');
  await page.goto('/api/auth/google/callback?error=access_denied');
  await expect(page.locator('[data-flash]')).toContainText('Вход через Google отменён');
});
