// node render.js — HTML + PNG (1440, 2x, целиком) в design/final/
const fs = require('fs'), path = require('path'); const { chromium } = require('playwright');
(async () => {
  const file = path.join(__dirname, 'desktop-01-home.html');
  fs.writeFileSync(file, require('./home')());
  const b = await chromium.launch(); const ctx = await b.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
  const p = await ctx.newPage(); await p.goto('file://' + file); await p.evaluate(() => document.fonts.ready); await p.waitForLoadState('networkidle'); await p.waitForTimeout(500);
  const over = await p.evaluate(() => document.documentElement.scrollWidth - innerWidth);
  const fams = await p.evaluate(() => [...new Set([...document.fonts].filter(x => x.status === 'loaded').map(x => x.family))].join(','));
  // проверка: текст вне подвала не меньше 18 px, нет переполнения строк
  const small = await p.evaluate(() => [...document.querySelectorAll('body *')].filter(e => !e.closest('footer') && !e.closest('svg') && [...e.childNodes].some(n => n.nodeType === 3 && n.textContent.trim()) && parseFloat(getComputedStyle(e).fontSize) < 17).map(e => e.tagName + ':' + getComputedStyle(e).fontSize + ':' + e.textContent.trim().slice(0, 30)));
  const clip = await p.evaluate(() => [...document.querySelectorAll('h1,h2,h3,p,b,.cat,.btn,.tag')].filter(e => e.scrollWidth > e.clientWidth + 1).map(e => e.textContent.trim().slice(0, 30)));
  const secs = await p.evaluate(() => [...document.querySelectorAll('section,footer')].map(s => s.className + ' ' + Math.round(s.getBoundingClientRect().top + scrollY) + '+' + Math.round(s.offsetHeight)));
  await p.screenshot({ path: path.join(__dirname, '..', 'desktop-01-home.png'), fullPage: true });
  console.log('h=' + await p.evaluate(() => document.documentElement.scrollHeight), over > 0 ? 'OVERFLOW ' + over : 'ok', fams);
  console.log('small:', small.join(' | ') || 'нет'); console.log('clip:', clip.join(' | ') || 'нет'); console.log(secs.join('\n'));
  await b.close();
})();
