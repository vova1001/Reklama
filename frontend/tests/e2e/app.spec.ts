import { test, expect, type Page } from '@playwright/test';
import { execSync } from 'node:child_process';

// Приложение после входа: режимы «Бизнес / Автор», профили режимов, меню аватара, настройки, выход, адаптив.
// Вход — по почте с кодом из журнала бэкенда (режим разработки, MAIL_PROVIDER=log).
const repo = new URL('../../../', import.meta.url).pathname;

function codeFor(email: string): string {
  const logs = execSync('docker compose logs backend --no-log-prefix --since 10m', { cwd: repo, encoding: 'utf8', maxBuffer: 32 << 20 });
  const line = logs.split('\n').filter((l) => l.includes(`"to":"${email}"`) && l.includes('Код для входа')).at(-1);
  const m = line?.match(/Код для входа в Охват: (\d{6})/);
  if (!m) throw new Error(`нет кода в журнале для ${email}`);
  return m[1];
}

const uniq = (p: string, name: string) => `e2e-app-${name}-${p}-${Date.now()}-${Math.floor(Math.random() * 1e6)}@example.com`;

function jsErrors(page: Page) {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  return errors;
}

async function login(page: Page, email: string) {
  await page.goto('/login');
  await page.getByRole('button', { name: 'Войти по почте' }).click();
  await page.getByLabel('Почта').fill(email);
  await page.getByRole('button', { name: 'Получить код' }).click();
  await expect(page.getByRole('heading', { name: 'Введите код' })).toBeVisible();
  await page.locator('[data-digit]').first().fill(codeFor(email));
  await page.waitForURL('**/app');
}

test.beforeEach(async ({ context }) => {
  await context.route('https://telegram.org/**', (r) => r.abort());
});

test('без входа приложение недоступно; API режима — только с сессией и CSRF', async ({ page, request }) => {
  for (const path of ['/app', '/app/profile', '/app/settings']) {
    await page.goto(path);
    await expect(page).toHaveURL(/\/login$/);
  }
  expect((await request.get('/api/me')).status()).toBe(401);
  expect((await request.patch('/api/me/mode', { data: { mode: 'creator' } })).status()).toBe(403);
});

test('режим: переключатель в шапке, лента и подсказки меняются, выбор сохраняется', async ({ page }, info) => {
  const errors = jsErrors(page);
  await login(page, uniq(info.project.name, 'mode'));
  const app = page.locator('[data-app]');
  await expect(app).toHaveAttribute('data-mode', 'business');
  // бизнес: пустая лента с «Создать задание» и пример своего задания, мягкая подсказка
  await expect(page.getByRole('heading', { level: 1, name: 'Ваши задания' })).toBeVisible();
  await expect(page.locator('[data-create-task]')).toHaveAttribute('href', '/tasks/new');
  await expect(page.locator('[data-nudge="business"]')).toBeVisible();
  await expect(page.locator('[data-progress-num]')).toHaveText('0%');

  // переключаем на «Автор» — без перезагрузки
  const sw = page.locator('#mode-bar');
  await sw.getByRole('radio', { name: 'Автор' }).click();
  await expect(app).toHaveAttribute('data-mode', 'creator');
  await expect(sw.getByRole('radio', { name: 'Автор' })).toHaveAttribute('aria-checked', 'true');
  await expect(page.getByRole('heading', { level: 1, name: 'Задания для вас' })).toBeVisible();
  await expect(page.getByRole('heading', { level: 1, name: 'Ваши задания' })).toBeHidden();
  await expect(page.locator('[data-toast]')).toHaveText('Режим: Автор');
  // демо-задания помечены «Пример»; отклик — мягкая подсказка, не блокирует
  const cards = page.locator('[data-for-mode="creator"] .task');
  await expect(cards).toHaveCount(3);
  for (const c of await cards.all()) await expect(c.locator('.pill-badge.warn')).toHaveText('Пример');
  await expect(page.locator('[data-nudge="creator"]')).toContainText('Заполните профиль автора, чтобы откликаться');
  await cards.first().getByRole('button', { name: 'Откликнуться' }).click();
  await expect(cards.first().locator('[data-respond-note]')).toContainText('заполните профиль автора');
  // подсказку можно скрыть
  await page.locator('[data-nudge="creator"] [data-nudge-close]').click();
  await expect(page.locator('[data-nudge="creator"]')).toBeHidden();

  // режим сохранён на сервере
  await page.reload();
  await expect(app).toHaveAttribute('data-mode', 'creator');
  const me = await (await page.request.get('/api/me')).json();
  expect(me.user.mode).toBe('creator');
  // стрелками — обратно в «Бизнес»
  await sw.getByRole('radio', { name: 'Автор' }).focus();
  await page.keyboard.press('ArrowLeft');
  await expect(app).toHaveAttribute('data-mode', 'business');
  await expect(sw.getByRole('radio', { name: 'Бизнес' })).toBeFocused();
  await expect.poll(async () => (await (await page.request.get('/api/me')).json()).user.mode).toBe('business');
  expect(errors).toEqual([]);
});

test('профиль: общие данные, профили бизнеса и автора, прогресс растёт', async ({ page }, info) => {
  const errors = jsErrors(page);
  await login(page, uniq(info.project.name, 'prof'));
  await page.goto('/app/profile');
  await expect(page.getByRole('heading', { level: 1, name: 'Профиль' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Выйти' }).first()).toBeVisible();

  // общие данные: пусто → ошибки рядом с полями → сохранено
  const basics = page.locator('[data-form="basics"]');
  await basics.getByRole('button', { name: 'Сохранить' }).click();
  await expect(basics.locator('[data-err="name"]')).not.toBeEmpty();
  await expect(basics.locator('[data-err="city"]')).toHaveText('Укажите город');
  await basics.getByLabel('Как вас называть').fill('Анна');
  await basics.getByLabel('Город').fill('Казань');
  await basics.getByRole('button', { name: 'Сохранить' }).click();
  await expect(basics.locator('[data-saved]')).toHaveText('Сохранено');
  await expect(page.locator('[data-step="basics"]')).toHaveAttribute('data-done', 'true');

  // вкладки: по умолчанию — режим «Бизнес»; стрелкой — «Автор»
  const tBiz = page.getByRole('tab', { name: /Бизнес/ });
  const tCre = page.getByRole('tab', { name: /Автор/ });
  await expect(tBiz).toHaveAttribute('aria-selected', 'true');
  await tBiz.focus();
  await page.keyboard.press('ArrowRight');
  await expect(tCre).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('#panel-creator')).toBeVisible();
  await expect(page.locator('#panel-business')).toBeHidden();

  // автор: темы, каналы (ошибка сервера рядом с полем), сохранение
  const cre = page.locator('[data-form="creator"]');
  await expect(cre.getByLabel('Имя или псевдоним')).toHaveValue('Анна');
  await cre.getByRole('button', { name: 'Сохранить профиль автора' }).click();
  await expect(cre.locator('[data-err="spheres"]')).toContainText('хотя бы одну');
  await expect(cre.locator('[data-err="channels"]')).toContainText('хотя бы одну');
  const chips = cre.locator('[data-slug]');
  for (let i = 0; i < 6; i++) await chips.nth(i).click();
  await expect(cre.locator('[data-err="spheres"]')).toHaveText('Можно выбрать до 5');
  await expect(cre.locator('[data-slug][aria-pressed="true"]')).toHaveCount(5);
  await cre.getByLabel('Канал 1').fill('javascript:alert(1)');
  await cre.getByRole('button', { name: 'Сохранить профиль автора' }).click();
  await expect(cre.locator('[data-err="channels"]')).toContainText('https://');
  await cre.getByLabel('Канал 1').fill('youtube.com/@anna');
  await cre.getByRole('button', { name: 'Добавить ещё' }).click();
  await cre.getByLabel('Канал 2').fill('@anna_tg');
  await cre.getByRole('button', { name: 'Сохранить профиль автора' }).click();
  await expect(cre.locator('[data-saved]')).toHaveText('Профиль автора сохранён');
  await expect(page.locator('[data-filled="creator"]')).toHaveClass(/is-on/);
  const pc = await (await page.request.get('/api/me/profile/creator')).json();
  expect(pc.profile.channels).toEqual(['https://youtube.com/@anna', '@anna_tg']);
  expect(pc.profile.spheres).toHaveLength(5);

  // бизнес
  await tBiz.click();
  const biz = page.locator('[data-form="business"]');
  await biz.getByLabel('Название компании или бренда').fill('Кофейня «Зерно»');
  await expect(biz.getByLabel('Город')).toHaveValue('Казань');
  await biz.locator('[data-slug]').first().click();
  await biz.getByRole('button', { name: 'Сохранить профиль бизнеса' }).click();
  await expect(biz.locator('[data-saved]')).toHaveText('Профиль бизнеса сохранён');
  expect((await (await page.request.get('/api/me/profile/business')).json()).profile.name).toBe('Кофейня «Зерно»');

  // после перезагрузки данные на месте; в ленте нет подсказки, прогресс 66% (нет только Telegram)
  await page.reload();
  await expect(page.locator('[data-form="business"]').getByLabel('Название компании или бренда')).toHaveValue('Кофейня «Зерно»');
  await page.goto('/app');
  await expect(page.locator('[data-nudge="business"]')).toBeHidden();
  await expect(page.locator('[data-progress-num]')).toHaveText('66%');
  await expect(page.locator('.hello')).toHaveText('Здравствуйте, Анна');
  expect(errors).toEqual([]);
});

test('меню аватара: клавиатура, Esc, режим и переходы', async ({ page }, info) => {
  await login(page, uniq(info.project.name, 'menu'));
  const btn = page.getByRole('button', { name: 'Меню профиля' });
  const menu = page.getByRole('menu', { name: 'Профиль' });
  await btn.focus();
  await page.keyboard.press('Enter');
  await expect(menu).toBeVisible();
  await expect(btn).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByRole('menuitem', { name: 'Профиль' })).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(page.getByRole('menuitemradio', { name: 'Бизнес' })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(menu).toBeHidden();
  await expect(btn).toBeFocused();
  // режим из меню
  await btn.click();
  await page.getByRole('menuitemradio', { name: 'Автор' }).click();
  await expect(page.locator('[data-app]')).toHaveAttribute('data-mode', 'creator');
  await btn.click();
  await expect(page.getByRole('menuitemradio', { name: 'Автор' })).toHaveAttribute('aria-checked', 'true');
  // щелчок мимо — закрывает
  await page.mouse.click(5, 300);
  await expect(menu).toBeHidden();
  // пункты ведут на страницы
  await btn.click();
  await page.getByRole('menuitem', { name: 'Настройки входа' }).click();
  await page.waitForURL('**/app/settings');
  await btn.click();
  await page.getByRole('menuitem', { name: 'Профиль' }).click();
  await page.waitForURL('**/app/profile');
  // профиль открывается на вкладке включённого режима
  await expect(page.getByRole('tab', { name: /Автор/ })).toHaveAttribute('aria-selected', 'true');
});

test('настройки: способы входа, уведомления, выход со всех устройств', async ({ page, browser, baseURL }, info) => {
  const errors = jsErrors(page);
  const email = uniq(info.project.name, 'set');
  await login(page, email);
  // тот же сеанс в другом браузере (новый код на ту же почту — не раньше чем через 60 с)
  const other = await browser.newContext({ baseURL });
  await other.addCookies((await page.context().cookies()).filter((c) => c.name === 'ohvat_session'));
  const p2 = await other.newPage();
  await p2.goto('/app');
  await expect(p2).toHaveURL(/\/app$/);

  await page.goto('/app/settings');
  await expect(page.locator('[data-provider="email"]')).toContainText('Подключён');
  await expect(page.locator('[data-provider="email"]')).toContainText(email);
  await expect(page.locator('[data-provider="google"]')).toContainText('Не подключён');
  await expect(page.locator('#notify')).toContainText('Telegram');
  await expect(page.getByRole('button', { name: 'Выйти', exact: true })).toBeVisible();

  // свой диалог подтверждения: «Отмена» — ничего не происходит
  await page.getByRole('button', { name: 'Выйти со всех устройств' }).click();
  const dlg = page.getByRole('dialog', { name: 'Выйти со всех устройств?' });
  await expect(dlg).toBeVisible();
  await dlg.getByRole('button', { name: 'Отмена' }).click();
  await expect(dlg).toBeHidden();
  expect((await page.request.get('/api/me')).status()).toBe(200);
  // подтвердили — оба устройства вышли
  await page.getByRole('button', { name: 'Выйти со всех устройств' }).click();
  await dlg.getByRole('button', { name: 'Выйти везде' }).click();
  await page.waitForURL('**/login');
  await p2.goto('/app');
  await expect(p2).toHaveURL(/\/login$/);
  await other.close();
  expect(errors).toEqual([]);
});

test('адаптив: нижняя навигация на телефоне, колонка «полезное» под лентой, без прокрутки вбок', async ({ page }, info) => {
  await login(page, uniq(info.project.name, 'resp'));
  for (const path of ['/app', '/app/profile', '/app/settings']) {
    await page.goto(path);
    for (const w of [320, 360, 375, 414, 768, 1024, 1440]) {
      await page.setViewportSize({ width: w, height: 800 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth), `${path} @${w}`).toBe(w);
    }
  }
  await page.goto('/app');
  // телефон: нижняя навигация, «полезное» — после ленты
  await page.setViewportSize({ width: 375, height: 760 });
  const nav = page.getByRole('navigation', { name: 'Разделы' });
  await expect(nav).toBeVisible();
  await expect(nav.getByRole('link', { name: 'Лента' })).toHaveAttribute('aria-current', 'page');
  const main = await page.locator('.app-main').boundingBox();
  const side = await page.locator('.app-side').boundingBox();
  expect(side!.y).toBeGreaterThan(main!.y + main!.height - 1);
  // кнопки нижней навигации — не меньше 44×44
  for (const l of await nav.getByRole('link').all()) {
    const b = await l.boundingBox();
    expect(b!.height).toBeGreaterThanOrEqual(44);
    expect(b!.width).toBeGreaterThanOrEqual(44);
  }
  await nav.getByRole('link', { name: 'Профиль' }).click();
  await page.waitForURL('**/app/profile');
  await expect(nav.getByRole('link', { name: 'Профиль' })).toHaveAttribute('aria-current', 'page');
  // компьютер: две колонки, нижней навигации нет, поиск в шапке
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/app');
  await expect(nav).toBeHidden();
  const m2 = await page.locator('.app-main').boundingBox();
  const s2 = await page.locator('.app-side').boundingBox();
  expect(s2!.x).toBeGreaterThan(m2!.x + m2!.width - 1);
  await page.locator('#q-bar').fill('кофейня');
  await page.keyboard.press('Enter');
  await expect(page.locator('[data-toast]')).toContainText('Поиск появится');
});

test('контраст: все видимые тексты приложения не ниже 4.5:1', async ({ page }, info) => {
  await login(page, uniq(info.project.name, 'contrast'));
  for (const path of ['/app', '/app/profile', '/app/settings']) {
    await page.goto(path);
    const bad = await page.evaluate(() => {
      const rgb = (c: string) => (c.match(/[\d.]+/g) ?? []).map(Number);
      const lum = ([r, g, b]: number[]) => {
        const f = (v: number) => ((v /= 255) <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
        return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
      };
      // фон — первый непрозрачный предок (полупрозрачный смешивается с белым)
      const bgOf = (el: Element | null): number[] => {
        for (let e = el; e; e = e.parentElement) {
          const [r, g, b, a = 1] = rgb(getComputedStyle(e).backgroundColor);
          if (a > 0.5) return [r, g, b].map((v) => v * a + 255 * (1 - a));
        }
        return [255, 255, 255];
      };
      const out: string[] = [];
      let checked = 0;
      const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
      for (let n = walker.nextNode(); n; n = walker.nextNode()) {
        const el = n.parentElement;
        if (!el || !n.textContent?.trim()) continue;
        const r = el.getBoundingClientRect();
        const cs = getComputedStyle(el);
        if (!r.width || !r.height || cs.visibility === 'hidden' || Number(cs.opacity) === 0 || el.closest('[hidden],[aria-hidden="true"],.sr-only,.toast,.skip,dialog:not([open])')) continue;
        const [fr, fg, fb, fa = 1] = rgb(cs.color);
        const bg = bgOf(el);
        const fgc = [fr, fg, fb].map((v, i) => v * fa + bg[i] * (1 - fa));
        const [l1, l2] = [lum(fgc), lum(bg)].sort((a, b) => b - a);
        const ratio = (l1 + 0.05) / (l2 + 0.05);
        checked++;
        if (ratio < 4.5) out.push(`${n.textContent.trim().slice(0, 30)} — ${ratio.toFixed(2)}`);
      }
      return { out, checked };
    });
    expect(bad.checked, path).toBeGreaterThan(20);
    expect(bad.out, path).toEqual([]);
  }
});
