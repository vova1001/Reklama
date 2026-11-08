// Скриншоты главной 375 / 768 / 1440 в project/screens/ (сайт должен быть поднят: BASE_URL, по умолчанию :8000).
import { chromium } from '@playwright/test';
import { mkdirSync } from 'node:fs';

const base = process.env.BASE_URL ?? 'http://localhost:8000';
const out = new URL('../../project/screens/', import.meta.url).pathname;
mkdirSync(out, { recursive: true });
const browser = await chromium.launch();
for (const [w, h, mobile] of [[375, 812, true], [768, 1024, false], [1440, 900, false]]) {
  // вся страница — в итоговом виде (без движения: подсветка слов, линии и превью уже «доиграли»)
  const ctx = await browser.newContext({ viewport: { width: w, height: h }, deviceScaleFactor: w === 1440 ? 1 : 2, isMobile: mobile, hasTouch: mobile, reducedMotion: 'reduce' });
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
  await ctx.close();
  // шапка-«пилюля» и превью «Топ ленты» с движением — после прокрутки к инструментам продвижения
  const live = await browser.newContext({ viewport: { width: w, height: h }, deviceScaleFactor: w === 1440 ? 1 : 2, isMobile: mobile, hasTouch: mobile });
  const lp = await live.newPage();
  await lp.goto(base + '/', { waitUntil: 'networkidle' });
  await lp.evaluate(async () => {
    const el = document.querySelector('.pr-top');
    const y = el.getBoundingClientRect().top + window.scrollY - (window.innerWidth < 768 ? 150 : 260);
    for (let v = 0; v < y; v += 300) { window.scrollTo(0, v); await new Promise((r) => setTimeout(r, 30)); }
    window.scrollTo(0, y);
  });
  await lp.waitForTimeout(3200);
  await lp.screenshot({ path: `${out}home-${w}-scrolled.png` });
  await live.close();
  console.log(`home-${w}.png`);
}
await browser.close();
