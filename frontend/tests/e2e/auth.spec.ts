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

async function requestCode(page: Page, email: string, role?: 'business' | 'creator') {
  await page.goto('/login');
  if (role) await page.getByRole('radio', { name: role === 'business' ? 'Я бизнес' : 'Я автор' }).click();
  await page.getByRole('checkbox').click();
  await page.getByLabel('Почта').fill(email);
  await page.getByRole('button', { name: 'Получить код' }).click();
  await expect(page.getByRole('heading', { name: 'Введите код' })).toBeVisible();
}

test('страница входа: роль, способы входа, согласие, без прокрутки вбок', async ({ page }) => {
  const errors = jsErrors(page);
  const res = await page.goto('/login');
  expect(res?.status()).toBe(200);
  await expect(page.locator('h1')).toHaveText(/Вход и\s*регистрация/);
  const roles = page.getByRole('radio');
  await expect(roles).toHaveCount(2);
  await expect(roles.first()).toHaveAttribute('aria-checked', 'true');
  // клавиатура: стрелка переключает роль
  await roles.first().focus();
  await page.keyboard.press('ArrowRight');
  await expect(roles.nth(1)).toHaveAttribute('aria-checked', 'true');
  await expect(roles.nth(1)).toBeFocused();
  // кнопки Google и Telegram — только если ключи заданы
  const cfg = await (await page.request.get('/api/auth/config')).json();
  await expect(page.getByRole('link', { name: 'Войти через Google' })).toHaveCount(cfg.google ? 1 : 0);
  await expect(page.getByRole('button', { name: 'Войти через Telegram' })).toHaveCount(cfg.telegram ? 1 : 0);
  // согласие — свой чекбокс, пробел переключает
  const chk = page.getByRole('checkbox');
  await chk.focus();
  await page.keyboard.press('Space');
  await expect(chk).toHaveAttribute('aria-checked', 'true');
  for (const w of [320, 360]) {
    await page.setViewportSize({ width: w, height: 740 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(w);
  }
  expect(errors).toEqual([]);
});

test('ошибки формы: пустая почта, неверный адрес, нет согласия, нет связи', async ({ page }) => {
  await page.goto('/login');
  const submit = page.getByRole('button', { name: 'Получить код' });
  await submit.click();
  await expect(page.locator('[data-err="email"]')).toHaveText('Введите почту');
  await expect(page.getByLabel('Почта')).toHaveAttribute('aria-invalid', 'true');
  await page.getByLabel('Почта').fill('anna@');
  await submit.click();
  await expect(page.locator('[data-err="email"]')).toContainText('Проверьте адрес почты');
  await page.getByLabel('Почта').fill('anna@mail.ru');
  await submit.click();
  await expect(page.locator('[data-err="consent"]')).toContainText('Отметьте согласие');
  await expect(page.getByRole('checkbox')).toBeFocused();
  // сеть пропала
  await page.route('**/api/auth/email/start', (r) => r.abort());
  await page.getByRole('checkbox').click();
  await submit.click();
  await expect(page.locator('[data-err="form"]')).toContainText('Нет связи с сервером');
  await expect(submit).toBeEnabled();
});

test('вход по почте → анкета автора → кабинет → выход', async ({ page }, info) => {
  const errors = jsErrors(page);
  const email = uniq(info.project.name, 'creator');
  await requestCode(page, email, 'creator');
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
  // верный — автопереход к анкете
  await page.locator('[data-digit]').first().fill(code);
  await page.waitForURL('**/login?step=profile');
  await expect(page.locator('h1')).toHaveText('Расскажите о себе');
  await expect(page.locator('.role-b')).toHaveText('Автор');

  // анкета: пустые поля
  await page.getByRole('button', { name: 'Готово' }).click();
  await expect(page.locator('[data-err="name"]')).not.toBeEmpty();
  await expect(page.locator('[data-err="city"]')).toHaveText('Укажите город');
  await expect(page.locator('[data-err="channels"]')).toContainText('хотя бы одну');
  await page.getByLabel('Имя или псевдоним').fill('Алина');
  await page.getByLabel('Город').fill('Москва');
  // неверная ссылка — ошибка от сервера рядом с полем
  await page.getByLabel('Канал 1').fill('javascript:alert(1)');
  await page.getByRole('button', { name: 'Готово' }).click();
  await expect(page.locator('[data-err="channels"]')).toContainText('https://');
  await page.getByLabel('Канал 1').fill('youtube.com/@alina');
  await page.getByRole('button', { name: 'Добавить ещё' }).click();
  await page.getByLabel('Канал 2').fill('@alina_tg');
  await page.getByRole('button', { name: 'Готово' }).click();

  await page.waitForURL('**/account');
  await expect(page.locator('h1')).toHaveText('Алина');
  await expect(page.locator('.rows')).toContainText('https://youtube.com/@alina'.replace('https://', ''));
  await expect(page.locator('.rows')).toContainText(email);
  // шапка: вместо «Войти» — аватар
  await expect(page.locator('[data-me]')).toHaveAttribute('href', '/account');
  await expect(page.locator('.bar .login')).toHaveCount(0);
  // /login для вошедшего с анкетой — сразу в кабинет
  await page.goto('/login');
  await expect(page).toHaveURL(/\/account$/);

  // cookie сессии — httpOnly, SameSite=Lax
  const sess = (await page.context().cookies()).find((c) => c.name === 'ohvat_session');
  expect(sess?.httpOnly).toBe(true);
  expect(sess?.sameSite).toBe('Lax');

  await page.getByRole('button', { name: 'Выйти' }).click();
  await page.waitForURL((u) => u.pathname === '/');
  await expect(page.locator('.bar .login')).toHaveCount(1);
  await page.goto('/account');
  await expect(page).toHaveURL(/\/login$/);
  expect(errors).toEqual([]);
});

test('бизнес: роль выбирается в анкете, сферы — пилюли до 5', async ({ page }, info) => {
  const email = uniq(info.project.name, 'biz');
  await page.goto('/login');
  // роль по умолчанию «бизнес» — сохраняется при регистрации
  await page.getByRole('checkbox').click();
  await page.getByLabel('Почта').fill(email);
  await page.getByRole('button', { name: 'Получить код' }).click();
  await expect(page.getByRole('heading', { name: 'Введите код' })).toBeVisible();
  await page.locator('[data-digit]').first().fill(codeFor(email));
  await page.waitForURL('**/login?step=profile');
  await expect(page.locator('.role-b')).toHaveText('Бизнес');
  await page.getByLabel('Название компании или бренда').fill('Кофейня «Зерно»');
  await page.getByLabel('Город').fill('Казань');
  await page.getByRole('button', { name: 'Готово' }).click();
  await expect(page.locator('[data-err="spheres"]')).toContainText('хотя бы одну');
  const chips = page.locator('[data-slug]');
  for (let i = 0; i < 6; i++) await chips.nth(i).click();
  await expect(page.locator('[data-err="spheres"]')).toHaveText('Можно выбрать до 5 сфер');
  await expect(page.locator('[data-slug][aria-pressed="true"]')).toHaveCount(5);
  await page.getByRole('button', { name: 'Готово' }).click();
  await page.waitForURL('**/account');
  await expect(page.locator('.rows .chip')).toHaveCount(5);
});

test('повторная отправка кода: таймер 60 с, лимит 429 с обратным отсчётом', async ({ page }, info) => {
  const email = uniq(info.project.name, 'limit');
  await requestCode(page, email);
  await expect(page.locator('[data-resend]')).toBeHidden();
  // назад и сразу ещё раз — сервер отвечает 429, кнопка показывает таймер
  await page.getByRole('button', { name: 'Другая почта' }).click();
  await page.getByRole('button', { name: 'Получить код' }).click();
  await expect(page.locator('[data-err="form"]')).toContainText('Новый код можно запросить через');
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
