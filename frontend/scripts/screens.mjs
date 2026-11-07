// Скриншоты главной 375 / 768 / 1440 в project/screens/ (сайт должен быть поднят: BASE_URL, по умолчанию :8000).
import { chromium } from '@playwright/test';
import { mkdirSync } from 'node:fs';

const base = process.env.BASE_URL ?? 'http://localhost:8000';
const out = new URL('../../project/screens/', import.meta.url).pathname;
mkdirSync(out, { recursive: true });
const browser = await chromium.launch();
for (const [w, h, mobile] of [[375, 812, true], [768, 1024, false], [1440, 900, false]]) {
  const ctx = await browser.newContext({ viewport: { width: w, height: h }, deviceScaleFactor: w === 1440 ? 1 : 2, isMobile: mobile, hasTouch: mobile });
  const page = await ctx.newPage();
  await page.goto(base + '/', { waitUntil: 'networkidle' });
  await page.evaluate(() => document.fonts.ready);
  // прокрутка до конца: подгрузка фото и появление блоков, затем наверх
  const total = await page.evaluate(() => document.documentElement.scrollHeight);
  for (let y = 0; y < total; y += 400) { await page.evaluate((v) => window.scrollTo(0, v), y); await page.waitForTimeout(120); }
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.waitForLoadState('networkidle');
  await page.waitForTimeout(2500);
  await page.screenshot({ path: `${out}home-${w}.png`, fullPage: true });
  await page.screenshot({ path: `${out}home-${w}-first-screen.png` });
  console.log(`home-${w}.png`);
  await ctx.close();
}
await browser.close();
