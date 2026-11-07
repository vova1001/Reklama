import { test, expect, type Page } from '@playwright/test';

// Ошибки консоли и страницы собираем на каждом тесте — их быть не должно.
function watchErrors(page: Page) {
  const errors: string[] = [];
  page.on('console', (m) => m.type() === 'error' && errors.push(m.text()));
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('response', (r) => r.status() >= 400 && errors.push(`${r.status()} ${r.url()}`));
  return errors;
}

test('главная открывается, все блоки на месте, ошибок нет', async ({ page }) => {
  const errors = watchErrors(page);
  const res = await page.goto('/');
  expect(res?.status()).toBe(200);
  await expect(page).toHaveTitle(/Охват/);
  await expect(page.locator('h1')).toHaveCount(1);
  await expect(page.locator('h1')).toContainText('Реклама у авторов видео');
  for (const h of ['Как это работает', 'Тарифы для бизнеса', 'Два вида заданий', 'Тарифы для авторов', 'Задания в любой теме', 'С чего начать', 'Частые вопросы', 'Разместите первое задание']) {
    await expect(page.getByRole('heading', { level: 2, name: new RegExp(h) })).toBeAttached();
  }
  await expect(page.locator('.pic img')).toHaveCount(4);
  await expect(page.locator('.st')).toHaveCount(4);
  // прокрутка до конца — подгружаются фото, проявляются блоки
  await page.evaluate(async () => {
    for (let y = 0; y < document.body.scrollHeight; y += 500) { window.scrollTo(0, y); await new Promise((r) => setTimeout(r, 40)); }
  });
  await expect(page.locator('footer')).toBeVisible();
  await page.waitForLoadState('networkidle');
  expect(errors).toEqual([]);
});

test('данные главной приходят из API', async ({ page, request }) => {
  const api = await request.get('/api/home');
  expect(api.ok()).toBeTruthy();
  const home = await api.json();
  expect(home.categories.length).toBeGreaterThan(0);
  await page.goto('/');
  await expect(page.locator('.cat')).toHaveCount(home.categories.length);
  await expect(page.locator('.biz .plan')).toHaveCount(home.businessPlans.length);
  await expect(page.locator('.aut .plan')).toHaveCount(home.creatorPlans.length);
  await expect(page.locator('.q')).toHaveCount(home.faq.length);
  await expect(page.locator('.biz .plan-on h3')).toHaveText(home.businessPlans.find((p: { recommended: boolean }) => p.recommended).name);
  await expect(page.locator('.cat').first()).toContainText(home.categories[0].title);
});

test('вопросы раскрываются и сворачиваются', async ({ page }) => {
  await page.goto('/');
  const q = page.locator('.q').nth(1);
  const btn = q.locator('button.h');
  await btn.scrollIntoViewIfNeeded();
  await expect(btn).toHaveAttribute('aria-expanded', 'false');
  await btn.click();
  await expect(btn).toHaveAttribute('aria-expanded', 'true');
  await expect(q.locator('.a p')).toBeVisible();
  await btn.click();
  await expect(btn).toHaveAttribute('aria-expanded', 'false');
});

test('кнопки ведут на будущие страницы «Скоро», неизвестный адрес — 404', async ({ page }) => {
  for (const path of ['/tasks', '/creators', '/tasks/new', '/creators/new', '/pricing', '/login']) {
    const res = await page.goto(path);
    expect(res?.status(), path).toBe(200);
    await expect(page.locator('h1')).toHaveText('Скоро');
  }
  const res = await page.goto('/no-such-page');
  expect(res?.status()).toBe(404);
  await page.goto('/');
  const hrefs = await page.locator('a[href^="/"]').evaluateAll((as) => [...new Set(as.map((a) => a.getAttribute('href')!.split('?')[0].split('#')[0]))]);
  for (const h of hrefs) {
    const r = await page.request.get(h || '/');
    expect(r.status(), h).toBe(200);
  }
});

test('нет горизонтальной прокрутки', async ({ page }) => {
  await page.goto('/');
  const over = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(over).toBeLessThanOrEqual(0);
});

test('мобильное меню открывается и закрывается', async ({ page, isMobile }) => {
  test.skip(!isMobile, 'меню проверяем на телефоне');
  await page.goto('/');
  const burger = page.locator('[data-burger]');
  await burger.click();
  await expect(burger).toHaveAttribute('aria-expanded', 'true');
  await expect(page.locator('[data-menu]')).toBeVisible();
  await page.locator('[data-menu] a', { hasText: 'Вопросы' }).click();
  await expect(burger).toHaveAttribute('aria-expanded', 'false');
});

test('шапка уплотняется при прокрутке', async ({ page }) => {
  await page.goto('/');
  const bar = page.locator('[data-bar]');
  await expect(bar).not.toHaveClass(/is-scrolled/);
  await page.mouse.wheel(0, 600);
  await expect(bar).toHaveClass(/is-scrolled/);
});

test.describe('уменьшение движения', () => {
  test.use({ reducedMotion: 'reduce' });
  test('всё видно сразу, без анимаций', async ({ page }) => {
    await page.goto('/');
    const op = await page.locator('.tile').first().evaluate((el) => getComputedStyle(el).opacity);
    expect(op).toBe('1');
    const word = await page.locator('.hero h1 .wd').first().evaluate((el) => getComputedStyle(el).opacity);
    expect(word).toBe('1');
  });
});
