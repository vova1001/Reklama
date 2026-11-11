import { test, expect, type Page } from '@playwright/test';

// Приложение после входа: всё служебное слева (навигация, режим, аккаунт), справа — одна колонка; на телефоне —
// шапка и нижняя навигация. Режимы «Бизнес / Автор», профиль, меню аккаунта, настройки, выход, адаптив, контраст.
// Вход — по почте с кодом из журнала бэкенда (режим разработки, MAIL_PROVIDER=log).
import { jsErrors, login, uniq } from './helpers';

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


/** Переключатель режима — в левой колонке (≥768) или в шапке (телефон). */
const modeSwitch = (page: Page) => page.locator('#mode-rail:visible, #mode-bar:visible').first();
const accountBtn = (page: Page) => page.locator('[data-menu-btn]:visible').first();

test('раскладка: служебное слева, справа одна колонка, правой колонки нет', async ({ page }, info) => {
  const errors = jsErrors(page);
  await login(page, uniq(info.project.name, 'shell'));
  const w = page.viewportSize()!.width;
  const col = await page.locator('main.col').boundingBox();
  if (w >= 768) {
    const rail = page.locator('.rail');
    await expect(rail).toBeVisible();
    const rb = (await rail.boundingBox())!;
    expect(rb.x).toBe(0);
    expect(col!.x).toBeGreaterThanOrEqual(rb.width - 1);
    // в колонке: навигация, режим, аккаунт
    await expect(rail.getByRole('link', { name: 'Лента', exact: true })).toHaveAttribute('aria-current', 'page');
    await expect(rail.locator('#mode-rail')).toBeVisible();
    await expect(rail.locator('[data-menu-btn]')).toBeVisible();
    await expect(page.locator('.mbar')).toBeHidden();
    await expect(page.locator('.bn')).toBeHidden();
  } else {
    await expect(page.locator('.rail')).toBeHidden();
    await expect(page.locator('.mbar')).toBeVisible();
    await expect(page.locator('.bn')).toBeVisible();
  }
  // содержимое — одна колонка не шире 600
  const content = await page.locator('main.col > section').boundingBox();
  expect(content!.width).toBeLessThanOrEqual(601);
  // правой колонки, виджетов прогресса, тарифа и «как это работает» нет
  await expect(page.locator('.app-side, [data-progress], .side-card')).toHaveCount(0);
  expect(await page.locator('main').textContent()).not.toMatch(/Как это работает|Тариф/);
  expect(errors).toEqual([]);
});

test('режим: переключатель слева (или в шапке), лента меняется, выбор сохраняется', async ({ page }, info) => {
  const errors = jsErrors(page);
  await login(page, uniq(info.project.name, 'mode'));
  const app = page.locator('[data-app]');
  await expect(app).toHaveAttribute('data-mode', 'business');
  // бизнес: свои задания — пусто, «Создать задание», тонкая строка-напоминание
  await expect(page.getByRole('heading', { level: 1, name: 'Ваши задания' })).toBeVisible();
  await expect(page.locator('[data-create-task]')).toHaveAttribute('href', '/app/tasks/new');
  await expect(page.locator('[data-nudge="business"]')).toBeVisible();
  await expect(page.locator('[data-nudge="business"]')).toContainText('Заполните профиль бизнеса');

  const sw = modeSwitch(page);
  await sw.getByRole('radio', { name: 'Автор' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Задания' })).toBeVisible();
  await expect(app).toHaveAttribute('data-mode', 'creator');
  // автор: опубликованных пока может не быть — пусто без выдуманных данных или настоящие задания
  await expect(page.locator('.pill-badge.warn, [data-demo]')).toHaveCount(0);
  // навигация по режиму: у автора — «Анкета», у бизнеса — «Создать» и «Мои»
  const nav = page.locator('.rail-nav:visible, .bn:visible').first();
  await expect(nav.getByRole('link', { name: 'Анкета' })).toBeVisible();
  await expect(nav.getByRole('link', { name: 'Мои' })).toBeHidden();
  // напоминание можно закрыть
  await page.locator('[data-nudge="creator"] [data-nudge-close]').click();
  await expect(page.locator('[data-nudge="creator"]')).toBeHidden();

  await page.reload();
  await expect(app).toHaveAttribute('data-mode', 'creator');
  expect((await (await page.request.get('/api/me')).json()).user.mode).toBe('creator');
  // стрелками — обратно в «Бизнес»
  await modeSwitch(page).getByRole('radio', { name: 'Автор' }).focus();
  await page.keyboard.press('ArrowLeft');
  await expect.poll(async () => (await (await page.request.get('/api/me')).json()).user.mode).toBe('business');
  await expect(page.getByRole('heading', { level: 1, name: 'Ваши задания' })).toBeVisible();
  const nav2 = page.locator('.rail-nav:visible, .bn:visible').first();
  await expect(nav2.getByRole('link', { name: 'Создать' })).toBeVisible();
  await expect(nav2.getByRole('link', { name: 'Мои' })).toBeVisible();
  expect(errors).toEqual([]);
});

test('профиль: общие данные, профили бизнеса и автора, напоминание пропадает', async ({ page }, info) => {
  const errors = jsErrors(page);
  await login(page, uniq(info.project.name, 'prof'));
  await page.goto('/app/profile');
  await expect(page.getByRole('heading', { level: 1, name: 'Профиль' })).toBeVisible();

  const basics = page.locator('[data-form="basics"]');
  await basics.getByRole('button', { name: 'Сохранить' }).click();
  await expect(basics.locator('[data-err="name"]')).not.toBeEmpty();
  await expect(basics.locator('[data-err="city"]')).toHaveText('Укажите город');
  await basics.getByLabel('Как вас называть').fill('Анна');
  await basics.getByLabel('Город').fill('Казань');
  await basics.getByRole('button', { name: 'Сохранить' }).click();
  await expect(basics.locator('[data-saved]')).toHaveText('Сохранено');

  const tBiz = page.getByRole('tab', { name: /Бизнес/ });
  const tCre = page.getByRole('tab', { name: /Автор/ });
  await tBiz.focus();
  await page.keyboard.press('ArrowRight');
  await expect(tCre).toHaveAttribute('aria-selected', 'true');

  const cre = page.locator('[data-form="creator"]');
  await expect(cre.getByLabel('Имя')).toHaveValue('Анна');
  await cre.getByRole('button', { name: 'Сохранить' }).click();
  await expect(cre.locator('[data-err="spheres"]')).toContainText('хотя бы одну');
  const chips = cre.locator('[data-slug]');
  for (let i = 0; i < 6; i++) await chips.nth(i).click();
  await expect(cre.locator('[data-err="spheres"]')).toHaveText('До 5');
  await cre.getByLabel('Канал 1').fill('javascript:alert(1)');
  await cre.getByRole('button', { name: 'Сохранить' }).click();
  await expect(cre.locator('[data-err="channels"]')).toContainText('https://');
  await cre.getByLabel('Канал 1').fill('youtube.com/@anna');
  await cre.getByRole('button', { name: 'Добавить ещё' }).click();
  await cre.getByLabel('Канал 2').fill('@anna_tg');
  await cre.getByRole('button', { name: 'Сохранить' }).click();
  await expect(cre.locator('[data-saved]')).toHaveText('Сохранено');
  const pc = await (await page.request.get('/api/me/profile/creator')).json();
  expect(pc.profile.channels).toEqual(['https://youtube.com/@anna', '@anna_tg']);

  await tBiz.click();
  const biz = page.locator('[data-form="business"]');
  await biz.getByLabel('Название').fill('Кофейня «Зерно»');
  await expect(biz.getByLabel('Город')).toHaveValue('Казань');
  await biz.locator('[data-slug]').first().click();
  await biz.getByRole('button', { name: 'Сохранить' }).click();
  await expect(biz.locator('[data-saved]')).toHaveText('Сохранено');

  await page.goto('/app');
  await expect(page.locator('[data-nudge="business"]')).toBeHidden();
  expect(errors).toEqual([]);
});

test('аватар профиля: загрузка с кадрированием — в меню и в профиле', async ({ page }, info) => {
  const errors = jsErrors(page);
  await login(page, uniq(info.project.name, 'ava'));
  await page.goto('/app/profile');
  await page.locator('[data-avatar] input[type=file]').setInputFiles(new URL('./fixtures/photo.jpg', import.meta.url).pathname);
  const dlg = page.getByRole('dialog', { name: 'Кадрирование' });
  await expect(dlg).toBeVisible();
  await dlg.getByRole('button', { name: 'Готово' }).click();
  await expect(page.locator('[data-avatar] .up-t[data-id]')).toHaveCount(1);
  await expect(page.locator('[data-toast]')).toContainText('Фото обновлено');
  const me = await (await page.request.get('/api/me')).json();
  expect(me.user.avatarUrl).toMatch(/^\/media\/[A-Za-z0-9_-]{22}\/320\.webp$/);
  // аватар виден всем (гостю тоже), квадрат WebP
  const res = await page.context().request.fetch(me.user.avatarUrl, { headers: { cookie: '' } });
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toBe('image/webp');
  expect(res.headers()['cache-control']).toContain('immutable');
  await page.reload();
  await expect(page.locator('.acc-btn:visible .ava img').first()).toHaveAttribute('src', me.user.avatarUrl);
  expect(errors).toEqual([]);
});

test('меню аккаунта: клавиатура, Esc, переходы, «Выйти»', async ({ page }, info) => {
  await login(page, uniq(info.project.name, 'menu'));
  const btn = accountBtn(page);
  const menu = page.locator('[data-menu]:visible');
  await btn.focus();
  await page.keyboard.press('Enter');
  await expect(menu).toBeVisible();
  await expect(btn).toHaveAttribute('aria-expanded', 'true');
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0);
  await expect(btn).toBeFocused();
  await btn.click();
  await page.getByRole('menuitem', { name: 'Настройки' }).click();
  await page.waitForURL('**/app/settings');
  await accountBtn(page).click();
  await page.getByRole('menuitem', { name: 'Профиль' }).click();
  await page.waitForURL('**/app/profile');
  await accountBtn(page).click();
  await page.getByRole('menuitem', { name: 'Выйти' }).click();
  await page.waitForURL((u) => u.pathname === '/');
  expect((await page.request.get('/api/me')).status()).toBe(401);
});

test('настройки: вход, уведомления, тариф, выход со всех устройств', async ({ page, browser, baseURL }, info) => {
  const errors = jsErrors(page);
  const email = uniq(info.project.name, 'set');
  await login(page, email);
  const other = await browser.newContext({ baseURL });
  await other.addCookies((await page.context().cookies()).filter((c) => c.name === 'ohvat_session'));
  const p2 = await other.newPage();
  await p2.goto('/app');
  await expect(p2).toHaveURL(/\/app$/);

  await page.goto('/app/settings');
  await expect(page.locator('[data-provider="email"]')).toContainText(email);
  await expect(page.locator('[data-provider="email"] .row-v')).toHaveText('Есть');
  await expect(page.locator('[data-provider="google"] .row-v')).toHaveText('Нет');
  await expect(page.locator('#plan')).toContainText('Бесплатный');
  await page.getByRole('button', { name: 'Выйти со всех устройств' }).click();
  const dlg = page.getByRole('dialog', { name: 'Выйти со всех устройств?' });
  await dlg.getByRole('button', { name: 'Отмена' }).click();
  await expect(dlg).toBeHidden();
  expect((await page.request.get('/api/me')).status()).toBe(200);
  await page.getByRole('button', { name: 'Выйти со всех устройств' }).click();
  await dlg.getByRole('button', { name: 'Выйти везде' }).click();
  await page.waitForURL('**/login');
  await p2.goto('/app');
  await expect(p2).toHaveURL(/\/login$/);
  await other.close();
  expect(errors).toEqual([]);
});

test('адаптив 320–1440: без прокрутки вбок, кнопки навигации не меньше 44×44', async ({ page }, info) => {
  await login(page, uniq(info.project.name, 'resp'));
  for (const path of ['/app', '/app/profile', '/app/settings', '/app/tasks/new', '/app/tasks']) {
    await page.goto(path);
    for (const w of [320, 360, 375, 414, 768, 1024, 1200, 1440]) {
      await page.setViewportSize({ width: w, height: 800 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth), `${path} @${w}`).toBe(w);
    }
  }
  await page.setViewportSize({ width: 375, height: 760 });
  await page.goto('/app');
  const nav = page.locator('.bn');
  for (const l of await nav.getByRole('link').all()) {
    if (!(await l.isVisible())) continue;
    const b = (await l.boundingBox())!;
    expect(b.height).toBeGreaterThanOrEqual(44);
    expect(b.width).toBeGreaterThanOrEqual(44);
  }
  await nav.getByRole('link', { name: 'Профиль' }).click();
  await page.waitForURL('**/app/profile');
  await expect(nav.getByRole('link', { name: 'Профиль' })).toHaveAttribute('aria-current', 'page');
  // планшет: колонка — только иконки, подписи — подсказкой при наведении
  await page.setViewportSize({ width: 900, height: 800 });
  const feed = page.locator('.rail .nv').first();
  expect((await feed.boundingBox())!.width).toBeLessThan(60);
  await feed.hover();
  expect(await feed.evaluate((el) => getComputedStyle(el, '::after').content)).toBe('"Лента"');
});

test('контраст: все видимые тексты приложения не ниже 4.5:1', async ({ page }, info) => {
  await login(page, uniq(info.project.name, 'contrast'));
  for (const path of ['/app', '/app/profile', '/app/settings', '/app/tasks/new', '/app/tasks']) {
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
    expect(bad.checked, path).toBeGreaterThan(4);
    expect(bad.out, path).toEqual([]);
  }
});
