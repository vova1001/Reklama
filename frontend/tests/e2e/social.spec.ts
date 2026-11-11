import { test, expect, type Page } from '@playwright/test';
import { jsErrors, login, uniq } from './helpers';
import { botOn, chatId, connectTelegram, FAKE_TG, hook, member, say, setChat, startTelegram, tap, tgUser, type Chat } from './social';

// Подтверждение соцсетей автора (задача 12): Telegram — бот-админ канала или группы (подставной Bot API, действия
// человека — вебхуком); YouTube в e2e без ключей Google — «Скоро», как и VK, TikTok, Instagram, Threads.
// Цифры — только от площадки: ручного ввода подписчиков в анкете нет.

test.beforeEach(async ({ context, request }) => {
  test.skip(!(await botOn(request)), 'бот выключен — поднимите стек: make e2e-up');
  await context.route('https://telegram.org/**', (r) => r.abort());
  await context.route('https://t.me/**', (r) => r.fulfill({ body: 'telegram' }));
});

async function creator(page: Page, name: string, project: string) {
  await login(page, uniq(project, name));
  const h = { 'X-CSRF-Token': (await (await page.request.get('/api/auth/config')).json()).csrfToken as string };
  expect((await page.request.patch('/api/me/mode', { headers: h, data: { mode: 'creator' } })).status()).toBe(200);
  await page.goto('/app/card');
  await expect(page.locator('[data-editor][data-ready]')).toHaveCount(1);
  await expect(page.locator('[data-social] .soc-p')).toHaveCount(6);
}

async function sentTo(chat: number, text: string) {
  await expect
    .poll(async () => {
      const calls = (await (await fetch(`${FAKE_TG}/__calls`)).json()) as { method: string; params: { chat_id?: number; text?: string } }[];
      return calls.some((c) => c.params.chat_id === chat && (c.params.text ?? '').includes(text));
    }, { timeout: 10_000 })
    .toBe(true);
}

test('Telegram: канал и группа подтверждаются ботом, цифры — от Telegram; «Скоро» у остальных', async ({ page, request }, info) => {
  test.slow();
  const errors = jsErrors(page);
  await creator(page, 'social', info.project.name);
  const sec = page.locator('#social');
  await expect(sec.getByRole('heading', { name: 'Соцсети' })).toBeVisible();
  // ручного ввода подписчиков нет
  await expect(page.getByLabel('Подписчики')).toHaveCount(0);
  // без ключей — «Скоро», кнопка недоступна
  for (const p of ['youtube', 'vk', 'tiktok', 'instagram', 'threads']) {
    const act = sec.locator(`[data-platform="${p}"] [data-act]`);
    await expect(act).toHaveText('Скоро');
    await expect(act).toHaveAttribute('aria-disabled', 'true');
  }
  const tg = sec.locator('[data-platform="telegram"]');
  await expect(tg.locator('.soc-s')).toHaveText('Не подключено');

  // «Подключить» → 3 шага и «Открыть бота»
  const id = tgUser();
  const chan: Chat = { id: chatId(), type: 'channel', title: 'Кофе с Алиной', username: `coffee${id}`, members: 12_345 };
  const tok = await startTelegram(page);
  await expect(tg.locator('.soc-steps li')).toHaveCount(3);
  // бота добавили в канал раньше, чем привязали Telegram — бот просит привязать
  await setChat(request, chan, [{ id, status: 'creator' }]);
  await hook(request, member(id, chan, 'administrator'));
  await sentTo(id, 'откройте анкету на сайте');
  // /start ch_… → «Привязать?» → «Привязать» — канал, добавленный раньше, подтверждается сразу; анкета видит сама
  await hook(request, say(id, `/start ch_${tok}`));
  await sentTo(id, 'Привязать этот Telegram');
  await hook(request, tap(id, `sl:${tok}`));
  await sentTo(id, 'Подтверждено: «Кофе с Алиной»');
  const acc = tg.locator('.soc-a', { hasText: 'Кофе с Алиной' });
  await expect(acc.locator('.soc-badge')).toHaveText('Подтверждено', { timeout: 15_000 });
  await expect(acc.locator('.soc-as')).toContainText(/Канал · 12\s345 подписчиков · обновлено \d+ [а-я]+/);
  await expect(acc.getByRole('link', { name: 'Кофе с Алиной' })).toHaveAttribute('href', `https://t.me/coffee${id}`);
  await expect(tg.locator('.soc-s')).toHaveText('Подтверждено');
  await expect(tg.locator('.soc-steps')).toHaveCount(0);

  // группа: бот — участник, автор — админ; число участников
  const grp: Chat = { id: chatId(), type: 'supergroup', title: 'Чат кофеманов', members: 321 };
  await setChat(request, grp, [{ id, status: 'administrator' }]);
  await hook(request, member(id, grp, 'member'));
  await sentTo(id, 'Группа «Чат кофеманов» подтверждена');
  await page.reload();
  const g = tg.locator('.soc-a', { hasText: 'Чат кофеманов' });
  await expect(g.locator('.soc-as')).toContainText('Группа · 321 участник');
  await expect(tg.locator('.soc-s')).toHaveText('Подтверждено: 2');

  // чужой канал (автор не админ) — не подтверждается
  const alien: Chat = { id: chatId(), type: 'channel', title: 'Чужой канал', members: 1e6 };
  await setChat(request, alien, [{ id: id + 1, status: 'creator' }]);
  await hook(request, member(id, alien, 'administrator'));
  await sentTo(id, 'Не вижу вас среди админов «Чужой канал»');

  // «Проверить снова» — сразу после подтверждения рано
  await acc.getByRole('button', { name: 'Проверить снова' }).click();
  await expect(page.locator('[data-toast]')).toContainText('Цифры только что обновлены');

  // бота убрали из группы — «Нужна повторная проверка» с причиной
  await hook(request, member(id, grp, 'left'));
  await page.reload();
  await expect(g.locator('.soc-as')).toHaveText('Бот убран из канала или группы — добавьте его снова');
  await expect(g.locator('.soc-badge')).toHaveCount(0);

  // отвязать
  await g.getByRole('button', { name: 'Отвязать' }).click();
  await page.getByRole('dialog', { name: 'Отвязать?' }).getByRole('button', { name: 'Отвязать' }).click();
  await expect(tg.locator('.soc-a')).toHaveCount(1);
  const api = (await (await page.request.get('/api/me/social')).json()).platforms;
  expect(api.map((p: { platform: string }) => p.platform)).toEqual(['telegram', 'youtube', 'vk', 'tiktok', 'instagram', 'threads']);
  expect(api[0].accounts).toHaveLength(1);
  expect(api[0].accounts[0]).toMatchObject({ kind: 'channel', followers: 12345, status: 'verified' });
  expect(errors).toEqual([]);
});

test('Telegram: чужая ссылка — «Это не я»; ссылка одноразовая; Telegram другого аккаунта не привязывается', async ({ page, request }, info) => {
  await creator(page, 'social-guard', info.project.name);
  const id = tgUser();
  const tok = await startTelegram(page);
  await hook(request, tap(id, `sx:${tok}`));
  await sentTo(id, 'Привязка отменена');
  await hook(request, tap(id, `sl:${tok}`));
  await sentTo(id, 'Ссылка устарела');
  // этот Telegram уже привязан к другому аккаунту
  const tok2 = await startTelegram(page);
  const other = await page.context().browser()!.newContext({ baseURL: info.project.use.baseURL });
  const p2 = await other.newPage();
  await creator(p2, 'social-guard2', info.project.name);
  const tok3 = await startTelegram(p2);
  await hook(request, tap(id, `sl:${tok3}`));
  await sentTo(id, 'Telegram привязан');
  await hook(request, tap(id, `sl:${tok2}`));
  await sentTo(id, 'уже привязан к другому аккаунту');
  await other.close();
  expect((await (await page.request.get('/api/me/social')).json()).platforms[0].accounts).toHaveLength(0);
});

test('анкета: без подтверждённой соцсети не опубликовать; с ней — «Подтверждено» и цифры у бизнеса', async ({ page, request, browser }, info) => {
  test.slow();
  const errors = jsErrors(page);
  await creator(page, 'social-card', info.project.name);
  const city = `Тверь ${Date.now().toString(36)}`;
  await page.getByLabel('Имя').fill('Алина');
  await page.getByLabel('Город').fill(city);
  await page.locator('[data-cats] [data-slug]').first().click();
  await expect(page.locator('[data-left]')).toContainText('соцсети');
  await page.getByRole('button', { name: 'Опубликовать' }).click();
  await expect(page.locator('[data-err="platforms"]')).toHaveText('Подтвердите хотя бы одну соцсеть');
  await expect(page.locator('[data-platform="telegram"] [data-act]')).toBeFocused();
  const id = tgUser();
  const chan: Chat = { id: chatId(), type: 'channel', title: 'Канал Алины', username: `alina${id}`, members: 4_200 };
  await connectTelegram(page, request, id, chan);
  await expect(page.locator('[data-err="platforms"]')).toBeHidden();
  await expect(page.locator('[data-left]')).toBeHidden();
  // предпросмотр — подтверждённые цифры
  await page.locator('[data-view="preview"]').click();
  await expect(page.locator('[data-preview] .rows')).toContainText(/4\s200 подписчиков/);
  await page.locator('[data-view="edit"]').click();
  await page.getByRole('button', { name: 'Опубликовать' }).click();
  await page.waitForURL('**/app/card?sent=1');
  const card = (await (await page.request.get('/api/me/card')).json()).card;
  expect(card.status).toBe('published');
  expect(card.platforms).toMatchObject([{ kind: 'telegram', type: 'channel', title: 'Канал Алины', followers: 4200, verified: true }]);

  // бизнес: в ленте — значок, на странице автора — «Подтверждено» и дата обновления
  const u = info.project.use;
  const ctx = await browser.newContext({ baseURL: u.baseURL, viewport: u.viewport, isMobile: u.isMobile, hasTouch: u.hasTouch, userAgent: u.userAgent });
  const biz = await ctx.newPage();
  await login(biz, uniq(info.project.name, 'social-biz'));
  await biz.request.patch('/api/me/mode', { headers: { 'X-CSRF-Token': (await (await biz.request.get('/api/auth/config')).json()).csrfToken }, data: { mode: 'business' } });
  await biz.goto(`/app/creator/${card.id}`);
  const row = biz.locator('.rows [data-platform="telegram"]');
  await expect(row).toContainText('Канал Алины');
  await expect(row.locator('.soc-badge')).toHaveText('Подтверждено');
  await expect(row).toContainText(/Канал · обновлено \d+ [а-я]+/);
  await expect(row.locator('.row-v')).toHaveText(/4\s200/);
  await ctx.close();
  expect(errors).toEqual([]);
});
