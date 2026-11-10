// Скриншоты экранов входа 375 / 768 / 1440 → project/screens/auth-*.png.
// Сайт поднят локально (make dev, APP_ENV=development): код из письма берётся из журнала бэкенда (docker compose logs).
import { chromium } from '@playwright/test';
import { execSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';

const base = process.env.BASE_URL ?? 'http://localhost:8000';
const out = new URL('../../project/screens/', import.meta.url).pathname;
const repo = new URL('../../', import.meta.url).pathname;
mkdirSync(out, { recursive: true });

/** Последний код для почты из журнала бэкенда (режим разработки). */
function codeFor(email) {
  const logs = execSync('docker compose logs backend --no-log-prefix --since 10m', { cwd: repo, encoding: 'utf8', maxBuffer: 32 << 20 });
  const lines = logs.split('\n').filter((l) => l.includes('"to":"' + email + '"') && l.includes('Код для входа'));
  const m = lines.at(-1)?.match(/Код для входа в Охват: (\d{6})/);
  if (!m) throw new Error('нет кода в журнале для ' + email);
  return m[1];
}

const browser = await chromium.launch();
const shot = (page, name) => page.screenshot({ path: `${out}auth-${name}.png`, fullPage: true });
const stamp = Date.now();

for (const [w, h, mobile] of [[375, 812, true], [768, 1024, false], [1440, 900, false]]) {
  const ctx = await browser.newContext({ viewport: { width: w, height: h }, deviceScaleFactor: w === 1440 ? 1 : 2, isMobile: mobile, hasTouch: mobile, reducedMotion: 'reduce' });
  await ctx.route('https://telegram.org/**', (r) => r.abort()); // окно Telegram не нужно для снимков
  const page = await ctx.newPage();
  await page.goto(base + '/login', { waitUntil: 'networkidle' });
  await page.evaluate(() => document.fonts.ready);
  await shot(page, `login-${w}`);

  // пустая почта и нет согласия — ошибки рядом с полями
  await page.click('[data-submit]');
  await page.fill('#email', 'name');
  await page.click('[data-submit]');
  await shot(page, `login-errors-${w}`);

  // код
  const email = `screens-${w}-${stamp}@example.com`;
  await page.fill('#email', email);
  await page.click('[data-consent]');
  if (w !== 768) await page.click('[data-role="creator"]');
  await page.click('[data-submit]');
  await page.waitForSelector('[data-pane="code"]:not([hidden])');
  await shot(page, `code-${w}`);
  const code = codeFor(email);
  const bad = code === '000000' ? '111111' : '000000';
  await page.locator('[data-digit]').first().fill(bad);
  await page.waitForSelector('[data-err="code"]:not(:empty)');
  await shot(page, `code-error-${w}`);

  await page.locator('[data-digit]').first().fill(code);
  await page.waitForURL('**/login?step=profile');
  await page.evaluate(() => document.fonts.ready);
  await shot(page, `profile-${w === 768 ? 'choose' : 'creator'}-${w}`);

  if (w === 768) {
    // роль не выбрана при входе → выбирается в анкете (бизнес: сферы)
    await page.click('[data-role="business"]');
    await page.fill('#name', 'Кофейня «Зерно»');
    await page.fill('#city', 'Казань');
    for (const s of ['food', 'beauty']) await page.click(`[data-slug="${s}"]`);
    await shot(page, `profile-business-${w}`);
  } else {
    await page.fill('#name', 'Алина Смирнова');
    await page.fill('#city', 'Москва');
    await page.fill('.chan input', 'youtube.com/@alina');
    await page.click('[data-add-channel]');
    await page.locator('.chan input').nth(1).fill('@alina_tg');
  }
  await page.click('[data-profile-form] [data-submit]');
  await page.waitForURL('**/account');
  await page.evaluate(() => document.fonts.ready);
  await shot(page, `account-${w}`);
  await page.goto(base + '/', { waitUntil: 'networkidle' });
  await page.screenshot({ path: `${out}auth-header-${w}.png` });
  await ctx.close();
  console.log(`auth-*-${w}.png`);
}
await browser.close();
