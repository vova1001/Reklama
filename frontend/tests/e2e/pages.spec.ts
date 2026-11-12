import { test, expect, type Page } from '@playwright/test';
import { jsErrors, login, uniq } from './helpers';
import { botOn, chatId, connectTelegram, tgUser, type Chat } from './social';

// Страница бизнеса и профиль автора (задача 14): конструктор блоков (добавить, перетащить, удалить), фото, видео
// по ссылке, черновик без потерь, публикация (MODERATION_MODE=auto — сразу), страница по ссылке для всех, смена адреса
// (прежний — 301 на новый), ссылка на страницу бизнеса со страницы задания; автор — профиль с подтверждёнными каналами.

const photo = new URL('./fixtures/photo.jpg', import.meta.url).pathname;
const rnd = () => Math.random().toString(36).slice(2, 8);

async function csrf(page: Page) {
  return { 'X-CSRF-Token': (await (await page.request.get('/api/auth/config')).json()).csrfToken as string };
}

async function editorReady(page: Page) {
  await expect(page.locator('[data-page-editor][data-ready]')).toHaveCount(1);
}

/** Дождаться, пока страница сохранится на сервере (последний PUT /api/me/pages/…). */
async function pageSaved(page: Page) {
  await page.waitForResponse((r) => /\/api\/me\/pages\/(business|creator)$/.test(r.url()) && r.request().method() === 'PUT' && r.ok(), { timeout: 10_000 });
}

test('бизнес: страница из блоков с фото и видео → опубликована → видна по ссылке всем, прежний адрес ведёт на новый', async ({ page, browser }, info) => {
  test.slow();
  const errors = jsErrors(page);
  await login(page, uniq(info.project.name, 'bizpage'));
  const h = await csrf(page);
  expect((await page.request.put('/api/me/profile', { headers: h, data: { name: 'Анна', city: 'Казань' } })).status()).toBe(200);

  // из профиля — «Оформить» страницу бизнеса
  await page.goto('/app/profile');
  const row = page.locator('[data-my-page="business"]');
  await expect(row).toContainText('Ещё не оформлена');
  await row.getByRole('link', { name: 'Оформить' }).click();
  await page.waitForURL('**/app/page?kind=business');
  await editorReady(page);
  // новая страница: блоки по умолчанию, адрес — латиницей
  const blocks = page.locator('.pb');
  await expect(blocks).toHaveCount(3);
  await expect(blocks.nth(0)).toHaveAttribute('data-type', 'about');
  const slug = `zerno-${rnd()}`;
  await page.getByLabel('Адрес страницы').fill(`Zerno ${slug.slice(6)}`);
  await expect(page.getByLabel('Адрес страницы')).toHaveValue(slug);
  await expect(page.locator('[data-slug-url]')).toContainText(`/b/${slug}`);

  // шапка: название, город, сфера
  await page.getByLabel('Название').fill('Кофейня «Зерно»');
  await page.getByLabel('Город').first().fill('Казань');
  await page.locator('[data-cats] .chip[data-slug]').first().click();

  // блоки: текст, фото, контакты; добавить «Видео» (ссылка YouTube) и «Факты»
  await blocks.nth(0).locator('textarea').fill('Варим кофе с 2015 года.\n\nОбжариваем сами.');
  await blocks.nth(1).locator('input[type="file"]').setInputFiles(photo);
  await expect(blocks.nth(1).locator('.up-t[data-id]')).toHaveCount(1, { timeout: 20_000 });
  const contacts = page.locator('.pb[data-type="contacts"]');
  await contacts.getByLabel('Адрес', { exact: true }).fill('ул. Баумана, 1');
  await contacts.getByLabel('Сайт', { exact: true }).fill('zerno.example.com');
  await page.locator('.pb-new', { hasText: 'Видео' }).click();
  const video = page.locator('.pb[data-type="video"]');
  await video.getByRole('button', { name: 'Ссылка YouTube / VK' }).click();
  await video.getByLabel('Ссылка на видео').fill('https://evil.example.com/watch');
  await video.getByLabel('Ссылка на видео').blur();
  await expect(video.locator('.ferr').first()).toHaveText('Подойдёт ссылка на ролик YouTube или VK Видео');
  await video.getByLabel('Ссылка на видео').fill('https://youtu.be/dQw4w9WgXcQ');
  await page.locator('.pb-new', { hasText: 'Факты' }).click();
  const facts = page.locator('.pb[data-type="facts"]');
  await facts.locator('textarea').fill('10 лет на рынке\n3 кофейни');
  // одиночный блок, который уже есть, второй раз не добавить
  await expect(page.locator('.pb-new', { hasText: 'О компании' })).toHaveCount(0);
  // перетащить: «Факты» — на место выше «Видео» (клавиатурой на ручке)
  await facts.locator('.pb-grip').focus();
  await page.keyboard.press('ArrowUp');
  await expect(blocks.nth(3)).toHaveAttribute('data-type', 'facts');
  await expect(blocks.nth(4)).toHaveAttribute('data-type', 'video');
  // удалить и вернуть из сообщения
  await video.locator('.pb-del').click();
  await expect(page.locator('.pb[data-type="video"]')).toHaveCount(0);
  await page.locator('.toast-act').click();
  await expect(page.locator('.pb[data-type="video"]')).toHaveCount(1);
  await pageSaved(page);

  // обновили страницу — всё на месте (черновик + сохранённая страница)
  await page.reload();
  await editorReady(page);
  await expect(page.getByLabel('Название')).toHaveValue('Кофейня «Зерно»');
  await expect(page.locator('.pb').nth(3)).toHaveAttribute('data-type', 'facts');
  await expect(page.locator('.pb[data-type="video"]').getByLabel('Ссылка на видео')).toHaveValue('https://youtu.be/dQw4w9WgXcQ');
  await expect(page.locator('.pb[data-type="photos"] .up-t[data-id]')).toHaveCount(1);
  await expect(page.getByLabel('Адрес страницы')).toHaveValue(slug);

  // «Как увидят»
  await page.locator('[data-view="preview"]').click();
  await expect(page.locator('[data-preview] .pg-t')).toHaveText('Кофейня «Зерно»');
  await expect(page.locator('[data-preview] .pg-b')).toHaveCount(5);
  await page.locator('[data-view="edit"]').click();

  // опубликовать (MODERATION_MODE=auto — сразу)
  await page.locator('[data-submit]').click();
  await page.waitForURL('**/app/page?kind=business&sent=1');
  await expect(page.locator('.st[data-s="published"]')).toBeVisible();
  await page.getByRole('link', { name: 'Открыть страницу' }).click();
  await page.waitForURL(`**/b/${slug}`);
  const pg = page.locator('[data-page-view="business"]');
  await expect(pg.getByRole('heading', { level: 1 })).toHaveText('Кофейня «Зерно»');
  await expect(pg.locator('.pg-text').first()).toContainText('Обжариваем сами.');
  await expect(pg.locator('.pg-ph img')).toHaveCount(1);
  const frame = pg.locator('.pg-video iframe');
  await expect(frame).toHaveAttribute('src', 'https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ');
  await expect(frame).toHaveAttribute('sandbox', /allow-scripts/);
  await expect(pg.locator('.pg-contacts a[href="https://zerno.example.com"]')).toBeVisible();
  await expect(pg.locator('.pg-facts li')).toHaveCount(2);

  // гость (без входа) — видит страницу и фото
  const u = info.project.use;
  const guest = await browser.newContext({ baseURL: u.baseURL, viewport: u.viewport, isMobile: u.isMobile, hasTouch: u.hasTouch, userAgent: u.userAgent });
  const gp = await guest.newPage();
  const res = await gp.goto(`/b/${slug}`);
  expect(res?.status()).toBe(200);
  await expect(gp.getByRole('link', { name: 'Войти' })).toBeVisible();
  await expect(gp.getByRole('heading', { level: 1 })).toHaveText('Кофейня «Зерно»');
  const img = await gp.locator('.pg-ph img').getAttribute('src');
  expect((await gp.request.get(img!)).status()).toBe(200);
  const width = await gp.evaluate(() => document.documentElement.scrollWidth);
  expect(width).toBeLessThanOrEqual(u.viewport!.width);

  // задание этого бизнеса: на странице — в «Открытых заданиях», со страницы задания — ссылка на страницу бизнеса
  const by = new Date(Date.now() + 5 * 864e5).toISOString().slice(0, 10);
  const t = await page.request.post('/api/me/tasks', { headers: h, data: {
    title: 'Сторис о новом меню', city: 'Казань', nationwide: false, categories: ['food'], formats: ['reels'], access: 'open', minViews: 1000,
    rewardKopecks: 50000, publishBy: by, publishTime: '', blocks: [{ kind: 'text', title: 'Описание', items: ['Снять обзор'] }], cover: '', photos: [],
  } });
  expect(t.status()).toBe(201);
  const id = ((await t.json()) as { id: number }).id;
  expect((await page.request.post(`/api/me/tasks/${id}/submit`, { headers: h })).status()).toBe(200);
  await page.goto(`/app/task/${id}`);
  await page.locator('[data-brand-link]').click();
  await page.waitForURL(`**/b/${slug}`);
  await expect(page.locator('[data-block="tasks"] .tile')).toHaveCount(1);

  // новый адрес; прежний — 301 на новый, у гостя тоже
  const slug2 = `${slug}-new`;
  expect((await page.request.put('/api/me/pages/business/slug', { headers: h, data: { slug: slug2 } })).status()).toBe(200);
  const old = await gp.request.get(`/b/${slug}`, { maxRedirects: 0 });
  expect(old.status()).toBe(301);
  expect(old.headers().location).toBe(`/b/${slug2}`);
  await gp.goto(`/b/${slug}`);
  await gp.waitForURL(`**/b/${slug2}`);
  // нет такой — 404
  expect((await gp.goto(`/b/net-takoy-${rnd()}`))?.status()).toBe(404);
  await guest.close();
  expect(errors).toEqual([]);
});

test('автор: профиль с подтверждённым каналом, темами и анкетой — по ссылке /c/…', async ({ page, request }, info) => {
  test.skip(!(await botOn(request)), 'бот выключен — поднимите стек: make e2e-up');
  test.slow();
  await page.context().route('https://telegram.org/**', (r) => r.abort());
  await page.context().route('https://t.me/**', (r) => r.fulfill({ body: 'telegram' }));
  const errors = jsErrors(page);
  await login(page, uniq(info.project.name, 'crepage'));
  const h = await csrf(page);
  expect((await page.request.patch('/api/me/mode', { headers: h, data: { mode: 'creator' } })).status()).toBe(200);
  // канал подтверждается в анкете (задача 12)
  await page.goto('/app/card');
  await expect(page.locator('[data-social] .soc-p')).toHaveCount(6);
  const chan: Chat = { id: chatId(), type: 'channel', title: 'Кофе с Алиной', username: `coffee${rnd()}`, members: 12_345 };
  await connectTelegram(page, request, tgUser(), chan);

  await page.goto('/app/page');
  await editorReady(page);
  await expect(page.locator('.pb')).toHaveCount(5);
  await expect(page.locator('.pb[data-type="channels"] .pb-note')).toContainText('появятся здесь сами');
  await page.getByLabel('Имя').fill('Алина');
  await page.getByLabel('Город').fill('Казань');
  await page.locator('[data-cats] .chip[data-slug]').first().click();
  await page.locator('.pb[data-type="about"] textarea').fill('Снимаю обзоры кофеен.');
  await page.locator('.pb[data-type="topics"] .chip[data-slug]').nth(1).click();
  await page.locator('.pb[data-type="works"] textarea').first().fill('Обзор для «Зерна»');
  await page.locator('.pb-new', { hasText: 'Отзывы' }).click();
  await expect(page.locator('.pb[data-type="reviews"]')).toHaveCount(1);
  const slug = `alina-${rnd()}`;
  await page.getByLabel('Адрес страницы').fill(slug);
  await page.locator('[data-submit]').click();
  await page.waitForURL('**/app/page?kind=creator&sent=1');
  await expect(page.locator('.st[data-s="published"]')).toBeVisible();

  await page.goto(`/c/${slug}`);
  const pg = page.locator('[data-page-view="creator"]');
  await expect(pg.getByRole('heading', { level: 1 })).toHaveText('Алина');
  const ch = pg.locator('[data-block="channels"] [data-platform="telegram"]');
  await expect(ch).toContainText('Кофе с Алиной');
  await expect(ch.locator('.soc-badge')).toHaveText('Подтверждено');
  await expect(ch.locator('.row-v')).toHaveText(/12\s345/);
  await expect(pg.locator('[data-block="topics"] .chip')).toHaveCount(1);
  await expect(pg.locator('[data-block="reviews"]')).toContainText('Отзывов пока нет');
  // анкета не опубликована — блока «Анкета» у посетителя нет
  await expect(pg.locator('[data-block="card"]')).toHaveCount(0);
  expect(errors).toEqual([]);
});
