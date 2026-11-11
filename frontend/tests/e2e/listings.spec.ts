import { test, expect, type Page } from '@playwright/test';
import { jsErrors, login, uniq } from './helpers';

// Задание бизнеса и анкета автора: черновик сохраняется сам, динамические блоки, свои категории, фото,
// предпросмотр, отправка → «На проверке», статусы в «Мои задания».
const fixture = (name: string) => new URL(`./fixtures/${name}`, import.meta.url).pathname;
const saved = (page: Page) => expect(page.locator('[data-state]')).toContainText('Черновик сохранён', { timeout: 10_000 });

test.beforeEach(async ({ context }) => {
  await context.route('https://telegram.org/**', (r) => r.abort());
});

function dateIn(days: number) {
  const d = new Date();
  d.setDate(d.getDate() + days);
  return { ru: `${String(d.getDate()).padStart(2, '0')}.${String(d.getMonth() + 1).padStart(2, '0')}.${d.getFullYear()}`, iso: d.toISOString().slice(0, 10) };
}

test('задание: блоки, фото, предпросмотр → «На проверке»', async ({ page }, info) => {
  test.slow();
  const errors = jsErrors(page);
  await login(page, uniq(info.project.name, 'task'));
  await page.goto('/app/tasks/new');
  await expect(page.getByRole('heading', { level: 1, name: 'Новое задание' })).toBeVisible();
  const submit = page.getByRole('button', { name: 'На проверку' });
  await expect(submit).toHaveAttribute('aria-disabled', 'true');
  await expect(submit).toHaveAttribute('data-tip', 'Добавьте название');

  // первая правка создаёт черновик, адрес меняется на /app/tasks/{id}
  await page.getByLabel('Название').fill('Сторис о новом осеннем меню');
  await saved(page);
  await expect(page).toHaveURL(/\/app\/tasks\/\d+$/);
  const id = Number(page.url().split('/').pop());

  // сфера, город, формат, тип, условия, оплата, срок
  await page.locator('[data-cats] [data-slug]').first().click();
  await page.getByLabel('Город').fill('Казань');
  await page.getByRole('button', { name: 'Reels' }).click();
  await page.getByRole('button', { name: 'Shorts' }).click();
  await page.getByRole('radio', { name: 'С одобрением' }).click();
  await page.getByLabel('Просмотры от').fill('5000');
  await expect(page.getByLabel('Просмотры от')).toHaveValue('5 000');
  await page.getByLabel('Оплата').fill('3000');
  await page.getByRole('button', { name: 'Неделя' }).click();
  await expect(page.getByLabel('Срок')).toHaveValue(dateIn(7).ru);

  // блоки: пункты по Enter, свой блок, порядок с клавиатуры, удаление блока с «Вернуть»
  const blocks = page.locator('[data-blocks] .bk');
  await expect(blocks).toHaveCount(3);
  await blocks.nth(0).locator('.bi-add input').fill('Снимите завтрак при утреннем свете');
  await page.keyboard.press('Enter');
  await blocks.nth(1).locator('.bi-add input').fill('Интерьер');
  await page.keyboard.press('Enter');
  await page.keyboard.type('Сырники');
  await page.keyboard.press('Enter');
  await expect(blocks.nth(1).locator('.bi')).toHaveCount(2);
  await blocks.nth(1).locator('.bi').nth(1).locator('.grip').focus();
  await page.keyboard.press('ArrowUp');
  await expect(blocks.nth(1).locator('.bi-in').first()).toHaveValue('Сырники');
  await page.getByRole('button', { name: 'Список' }).click();
  await expect(blocks).toHaveCount(4);
  await page.keyboard.type('Хештеги');
  await blocks.nth(3).locator('.bi-add input').fill('#зерно');
  await page.keyboard.press('Enter');
  await blocks.nth(2).getByRole('button', { name: 'Удалить блок' }).click();
  await expect(blocks).toHaveCount(3);
  await expect(page.locator('[data-toast]')).toContainText('Блок удалён');
  await page.locator('[data-toast-act]').click();
  await expect(blocks).toHaveCount(4);
  await expect(blocks.nth(2).locator('.bk-title')).toHaveValue('Требования');
  await blocks.nth(2).getByRole('button', { name: 'Удалить блок' }).click();
  await expect(blocks).toHaveCount(3);

  // обложка — с кадрированием 16:9; два фото «что показать»
  await page.locator('[data-cover] input[type=file]').setInputFiles(fixture('photo.jpg'));
  await page.getByRole('dialog', { name: 'Кадрирование' }).getByRole('button', { name: 'Готово' }).click();
  await expect(page.locator('[data-cover] .up-t[data-id]')).toHaveCount(1, { timeout: 15_000 });
  await page.locator('[data-photos] input[type=file]').setInputFiles([fixture('photo.jpg'), fixture('portrait.jpg')]);
  await expect(page.locator('[data-photos] .up-t[data-id]')).toHaveCount(2, { timeout: 20_000 });
  await saved(page);

  // черновик на сервере — со всем введённым
  const draft = await (await page.request.get(`/api/me/tasks/${id}`)).json();
  expect(draft.status).toBe('draft');
  expect(draft.formats).toEqual(['reels', 'shorts']);
  expect(draft.access).toBe('approval');
  expect(draft.rewardKopecks).toBe(300000);
  expect(draft.publishBy).toBe(dateIn(7).iso);
  expect(draft.blocks.map((b: { title: string }) => b.title)).toEqual(['Описание', 'Что показать', 'Хештеги']);
  expect(draft.blocks[1].items).toEqual(['Сырники', 'Интерьер']);
  expect(draft.cover.id).toHaveLength(22);
  expect(draft.photos).toHaveLength(2);
  // фото черновика — только владельцу
  const guest = await page.context().browser()!.newContext();
  expect((await guest.request.get(new URL(draft.cover.src, page.url()).toString())).status()).toBe(404);
  await guest.close();
  expect((await page.request.get(draft.cover.src)).status()).toBe(200);

  // предпросмотр — как увидят авторы
  await page.getByRole('tab', { name: 'Как увидят' }).click();
  const pv = page.locator('[data-preview]');
  await expect(pv.locator('.it-t')).toHaveText('Сторис о новом осеннем меню');
  await expect(pv).toContainText(/3\s000\s₽/);
  await expect(pv).toContainText(/от 5\s000 просм\./);
  await expect(pv).toContainText('Reels, Shorts');
  await expect(pv.locator('.body-b h3')).toHaveText(['Описание', 'Что показать', 'Хештеги']);
  await expect(pv.locator('.it-cover')).toBeVisible();
  await page.getByRole('tab', { name: 'Правка' }).click();

  // на проверку
  await expect(submit).toHaveAttribute('aria-disabled', 'false');
  await submit.click();
  await page.waitForURL(`**/app/tasks/${id}?sent=1`);
  await expect(page.locator('[data-status-label]')).toHaveText('На проверке');
  await expect(page.locator('.banner')).toContainText('Отправлено');
  expect((await (await page.request.get(`/api/me/tasks/${id}`)).json()).status).toBe('pending_review');

  // в «Мои задания» и в ленте бизнеса — со статусом
  await page.goto('/app/tasks');
  const row = page.locator('.it', { hasText: 'Сторис о новом осеннем меню' });
  await expect(row.locator('.st')).toHaveText('На проверке');
  await page.goto('/app');
  await expect(page.locator('.it', { hasText: 'Сторис о новом осеннем меню' }).locator('.st')).toHaveText('На проверке');

  // вернуть в черновик — снова правится
  await page.goto(`/app/tasks/${id}`);
  await page.getByRole('button', { name: 'В черновик' }).click();
  await expect(page.getByLabel('Название')).toHaveValue('Сторис о новом осеннем меню');
  expect(errors).toEqual([]);
});

test('черновик: сохраняется сам, открывается из «Мои», удаляется', async ({ page }, info) => {
  const errors = jsErrors(page);
  await login(page, uniq(info.project.name, 'draft'));
  await page.goto('/app/tasks/new');
  await page.getByLabel('Название').fill('Черновик про кофе');
  await page.getByLabel('Лайки от').fill('300');
  await page.locator('[data-blocks] .bk').first().locator('.bi-add input').fill('Пока только идея');
  await page.keyboard.press('Enter');
  await saved(page);
  // ушли и вернулись — всё на месте
  await page.goto('/app/tasks');
  await page.getByRole('button', { name: /Черновик/ }).click();
  const row = page.locator('.it', { hasText: 'Черновик про кофе' });
  await expect(row.locator('.st')).toHaveText('Черновик');
  await row.getByRole('link', { name: 'Изменить' }).click();
  await expect(page.getByLabel('Название')).toHaveValue('Черновик про кофе');
  await expect(page.getByLabel('Лайки от')).toHaveValue('300');
  await expect(page.locator('[data-blocks] .bi-in').first()).toHaveValue('Пока только идея');
  // отправить нельзя — причина у кнопки; щелчок показывает, что заполнить
  const submit = page.getByRole('button', { name: 'На проверку' });
  await expect(submit).toHaveAttribute('aria-disabled', 'true');
  await submit.click({ force: true }); // aria-disabled: кнопка доступна для фокуса и щелчка — показывает, что заполнить
  await expect(page.locator('[data-err="category"]')).toHaveText('Выберите сферу');
  // в ленте бизнеса — строка «Черновики: 1»
  await page.goto('/app');
  await expect(page.locator('.nudge', { hasText: 'Черновики: 1' })).toBeVisible();
  // удалить
  await page.goto('/app/tasks');
  await page.locator('.it', { hasText: 'Черновик про кофе' }).getByRole('button', { name: 'Удалить' }).click();
  await page.getByRole('dialog', { name: 'Удалить задание?' }).getByRole('button', { name: 'Удалить' }).click();
  await expect(page.locator('.it', { hasText: 'Черновик про кофе' })).toHaveCount(0);
  expect((await (await page.request.get('/api/me/tasks')).json()).tasks).toHaveLength(0);
  expect(errors).toEqual([]);
});

test('анкета автора: своя тема, площадки, работы → «На проверке»', async ({ page }, info) => {
  test.slow();
  const errors = jsErrors(page);
  await login(page, uniq(info.project.name, 'card'));
  await page.locator('#mode-rail:visible, #mode-bar:visible').first().getByRole('radio', { name: 'Автор' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Задания', exact: true })).toBeVisible(); // лента перезагрузилась в режиме автора
  await page.goto('/app/card');
  await expect(page.getByRole('heading', { level: 1, name: 'Анкета' })).toBeVisible();
  const submit = page.getByRole('button', { name: 'На проверку' });
  await expect(submit).toHaveAttribute('aria-disabled', 'true');

  await page.getByLabel('Имя').fill('Алина');
  await page.getByLabel('Город').fill('Москва');
  // темы: общая и своя (подсказки похожих, без дублей)
  await page.locator('[data-cats] [data-slug]').first().click();
  await page.getByRole('button', { name: 'Своя' }).click();
  const cb = page.getByRole('combobox', { name: 'Своя категория' });
  await cb.fill('красот');
  await expect(page.getByRole('option', { name: 'Красота' })).toBeVisible();
  await cb.fill('Настольные игры');
  await page.getByRole('option', { name: 'Добавить «Настольные игры»' }).click();
  await expect(page.locator('[data-cats] .chip.is-own[aria-pressed="true"]')).toContainText('Настольные игры');
  await expect(page.locator('[data-cats] .chip.is-own')).toContainText('на проверке');

  // площадки: ссылка + подписчики
  const plats = page.locator('[data-platforms] .plat');
  await plats.first().getByLabel('Ссылка на площадку').fill('youtube.com/@alina');
  await plats.first().getByLabel('Подписчики').fill('12000');
  await expect(plats.first().getByLabel('Подписчики')).toHaveValue('12 000');
  await page.getByRole('button', { name: 'Площадка' }).click();
  await plats.nth(1).getByLabel('Ссылка на площадку').fill('t.me/alina');
  await plats.nth(1).getByLabel('Подписчики').fill('800');

  // блоки: о себе, форматы и цены, примеры — ссылки
  const blocks = page.locator('[data-blocks] .bk');
  expect(await blocks.locator('.bk-title').evaluateAll((els) => els.map((e) => (e as HTMLInputElement).value))).toEqual(['О себе', 'Форматы и цены', 'Примеры работ']);
  await blocks.nth(0).locator('.bi-add input').fill('Снимаю еду и городские места');
  await page.keyboard.press('Enter');
  await blocks.nth(1).locator('.bi-add input').fill('Reels — 5 000 ₽');
  await page.keyboard.press('Enter');
  await blocks.nth(2).locator('.bi-add input').fill('vk.com/clip-1');
  await page.keyboard.press('Enter');

  // аватар (кадр 1:1) и работы
  await page.locator('[data-cover] input[type=file]').setInputFiles(fixture('portrait.jpg'));
  await page.getByRole('dialog', { name: 'Кадрирование' }).getByRole('button', { name: 'Готово' }).click();
  await page.locator('[data-photos] input[type=file]').setInputFiles([fixture('photo.jpg')]);
  await expect(page.locator('[data-cover] .up-t[data-id], [data-photos] .up-t[data-id]')).toHaveCount(2, { timeout: 20_000 });
  await saved(page);

  const card = (await (await page.request.get('/api/me/card')).json()).card;
  expect(card.status).toBe('draft');
  expect(card.platforms.map((p: { kind: string }) => p.kind)).toEqual(['youtube', 'telegram']);
  expect(card.platforms[0].followers).toBe(12000);
  expect(card.topics).toHaveLength(2);
  expect(card.blocks[2].items).toEqual(['https://vk.com/clip-1']);
  expect(card.avatar.id).toHaveLength(22);
  expect(card.works).toHaveLength(1);

  await page.getByRole('tab', { name: 'Как увидят' }).click();
  await expect(page.locator('[data-preview]')).toContainText(/12\s000 подписч\./);
  await expect(page.locator('[data-preview]')).toContainText('Настольные игры');
  await page.getByRole('tab', { name: 'Правка' }).click();

  await submit.click();
  await page.waitForURL('**/app/card?sent=1');
  await expect(page.locator('[data-status-label]')).toHaveText('На проверке');
  expect((await (await page.request.get('/api/me/card')).json()).card.status).toBe('pending_review');
  // фото анкеты на проверке — только владельцу
  const guest = await page.context().browser()!.newContext();
  expect((await guest.request.get(new URL(card.works[0].src, page.url()).toString())).status()).toBe(404);
  await guest.close();
  expect(errors).toEqual([]);
});

test('фото: понятные ошибки типа и размера, отмена загрузки', async ({ page }, info) => {
  await login(page, uniq(info.project.name, 'up'));
  await page.goto('/app/tasks/new');
  const input = page.locator('[data-photos] input[type=file]');
  await input.setInputFiles(fixture('anim.gif'));
  await expect(page.locator('[data-toast]')).toContainText('JPEG, PNG, WebP или HEIC');
  await input.setInputFiles({ name: 'big.jpg', mimeType: 'image/jpeg', buffer: Buffer.alloc(11 << 20, 1) });
  await expect(page.locator('[data-toast]')).toContainText('больше 10 МБ');
  await expect(page.locator('[data-photos] .up-t')).toHaveCount(0);
  // не фото под видом JPEG — отказ сервера у плитки
  await input.setInputFiles({ name: 'fake.jpg', mimeType: 'image/jpeg', buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"/>') });
  await expect(page.locator('[data-photos] .up-t.is-error .up-err')).toContainText('JPEG, PNG, WebP или HEIC');
  // отмена: медленная сеть — «Отменить загрузку»
  await page.route('**/api/me/media', async (r) => {
    await new Promise((res) => setTimeout(res, 3000));
    await r.continue().catch(() => {});
  });
  await input.setInputFiles(fixture('photo.jpg'));
  await page.locator('[data-photos] .up-t.is-loading').getByRole('button', { name: 'Отменить загрузку' }).click();
  await expect(page.locator('[data-photos] .up-t.is-loading')).toHaveCount(0);
});

test('сервер проверяет сам: неполное — 400 по полям, чужое — 404, лишнее поле — 400', async ({ page }, info) => {
  await login(page, uniq(info.project.name, 'api'));
  const csrf = (await (await page.request.get('/api/auth/config')).json()).csrfToken;
  const h = { 'X-CSRF-Token': csrf };
  const base = { title: '', city: '', nationwide: false, category: '', formats: [], access: 'open', minViews: 0, minLikes: 0, rewardKopecks: 0, publishBy: '', blocks: [], cover: '', photos: [] };
  const created = await page.request.post('/api/me/tasks', { headers: h, data: base });
  expect(created.status()).toBe(201);
  const { id } = await created.json();
  const sub = await page.request.post(`/api/me/tasks/${id}/submit`, { headers: h });
  expect(sub.status()).toBe(400);
  const f = (await sub.json()).error.fields;
  for (const k of ['title', 'city', 'category', 'formats', 'minViews', 'reward', 'publishBy', 'blocks']) expect(f[k], k).toBeTruthy();
  const bad = await page.request.put(`/api/me/tasks/${id}`, { headers: h, data: { ...base, blocks: [{ kind: 'links', title: 'x', items: ['javascript:alert(1)'] }] } });
  expect((await bad.json()).error.fields['blocks.0']).toBeTruthy();
  expect((await page.request.put(`/api/me/tasks/${id}`, { headers: h, data: { ...base, status: 'published' } })).status()).toBe(400);
  expect((await page.request.get('/api/me/tasks/999999999')).status()).toBe(404);
});
