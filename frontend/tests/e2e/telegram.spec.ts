import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

// Вход через бота Telegram: сайт поднят с подставным Bot API (make e2e-up, docker-compose.e2e.yml).
// Действия человека в Telegram эмулируются вебхуком — так же, как их присылает Telegram.
const SECRET = process.env.E2E_TG_SECRET ?? 'e2e-webhook-secret';
const FAKE_TG = process.env.E2E_FAKE_TG ?? 'http://localhost:8081';

type Call = { method: string; params: Record<string, unknown> };
let updateId = Date.now() % 1e9;

async function botOn(request: APIRequestContext) {
  const cfg = await (await request.get('/api/auth/config')).json();
  return !!cfg.telegram?.bot;
}

function hook(request: APIRequestContext, update: Record<string, unknown>, secret = SECRET) {
  return request.post('/api/telegram/webhook', {
    headers: { 'X-Telegram-Bot-Api-Secret-Token': secret },
    data: { update_id: ++updateId, ...update },
  });
}

const from = (id: number) => ({ id, is_bot: false, first_name: 'Тест', last_name: 'Телеграмов', username: `t${id}` });
const say = (id: number, text: string) => ({ message: { message_id: 1, from: from(id), chat: { id, type: 'private' }, text } });
const tap = (id: number, data: string) => ({
  callback_query: { id: `cb${id}${updateId}`, from: from(id), data, message: { message_id: 7, chat: { id, type: 'private' } } },
});

async function calls(request: APIRequestContext, chat: number): Promise<Call[]> {
  const all = (await (await request.get(`${FAKE_TG}/__calls`)).json()) as Call[];
  return all.filter((c) => c.params.chat_id === chat);
}

async function waitCall(request: APIRequestContext, chat: number, match: (c: Call) => boolean) {
  await expect.poll(async () => (await calls(request, chat)).some(match), { timeout: 10_000 }).toBe(true);
  return (await calls(request, chat)).filter(match).at(-1)!;
}

// Нажать «Войти через Telegram» и получить токен из ссылки на бота.
async function startBotLogin(page: Page): Promise<string> {
  await page.getByRole('button', { name: 'Войти через Telegram' }).click();
  await expect(page.getByRole('heading', { name: 'Подтвердите вход в Telegram' })).toBeVisible();
  const href = await page.locator('[data-tg-open]').getAttribute('href');
  const m = href?.match(/^https:\/\/t\.me\/ohvat_test_bot\?start=login_([A-Za-z0-9_-]{32})$/);
  expect(m, `ссылка на бота: ${href}`).toBeTruthy();
  expect(href!.split('start=')[1].length).toBeLessThanOrEqual(64);
  await expect(page.locator('[data-tg-wait]')).toContainText(/Ждём подтверждения · [45]:\d\d/);
  return m![1];
}

test.beforeEach(async ({ context, request }) => {
  test.skip(!(await botOn(request)), 'бот выключен — поднимите стек: make e2e-up');
  await context.route('https://t.me/**', (r) => r.fulfill({ body: 'telegram' })); // вкладку Telegram не открываем по-настоящему
  await context.route('https://telegram.org/**', (r) => r.abort());
});

test('вход через бота: /start → «Подтвердить вход» → сразу приложение', async ({ page, request }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  const tg = 7_000_000 + Math.floor(Math.random() * 1e6);
  await page.goto('/login');
  const token = await startBotLogin(page);
  // пока не подтверждено — страница ждёт
  await page.waitForTimeout(500);
  await expect(page).toHaveURL(/\/login$/);

  // человек открыл бота: /start login_<токен>
  expect((await hook(request, say(tg, `/start login_${token}`))).status()).toBe(200);
  const ask = await waitCall(request, tg, (c) => c.method === 'sendMessage' && String(c.params.text).includes('Подтвердите'));
  const kb = (ask.params.reply_markup as { inline_keyboard: { text: string; callback_data: string }[][] }).inline_keyboard;
  expect(kb[0][0]).toMatchObject({ text: 'Подтвердить вход', callback_data: `ok:${token}` });
  expect(kb[1][0]).toMatchObject({ text: 'Это не я', callback_data: `no:${token}` });

  // нажал «Подтвердить вход» → сайт сам открывает приложение
  expect((await hook(request, tap(tg, `ok:${token}`))).status()).toBe(200);
  await page.waitForURL('**/app', { timeout: 15_000 });
  await expect(page.locator('[data-app]')).toBeVisible();
  await waitCall(request, tg, (c) => c.method === 'editMessageText' && String(c.params.text).includes('Вход подтверждён'));
  // Telegram привязан — шаг «Уведомления в Telegram» выполнен
  await expect(page.locator('[data-step="telegram"]')).toHaveAttribute('data-done', 'true');
  await page.goto('/app/settings');
  await expect(page.locator('[data-provider="telegram"]')).toContainText('Подключён');

  // ссылка одноразовая: повторное нажатие — «устарела»
  await hook(request, tap(tg, `ok:${token}`));
  await waitCall(request, tg, (c) => c.method === 'editMessageText' && String(c.params.text).includes('устарела'));
  // /profile в боте — кто я и какой режим
  await hook(request, say(tg, '/profile'));
  await waitCall(request, tg, (c) => c.method === 'sendMessage' && String(c.params.text).includes('Режим: бизнес'));

  // второй вход тем же Telegram — тот же аккаунт, сразу приложение
  await page.context().clearCookies();
  await page.goto('/login');
  const t2 = await startBotLogin(page);
  await hook(request, say(tg, `/start login_${t2}`));
  await hook(request, tap(tg, `ok:${t2}`));
  await page.waitForURL('**/app', { timeout: 15_000 });
  await expect(page.locator('.hello')).toContainText('Тест');
  expect(errors).toEqual([]);
});

test('«Это не я» в боте — страница показывает отмену и даёт новую ссылку', async ({ page, request }) => {
  const tg = 8_000_000 + Math.floor(Math.random() * 1e6);
  await page.goto('/login');
  const token = await startBotLogin(page);
  await hook(request, say(tg, `/start login_${token}`));
  await hook(request, tap(tg, `no:${token}`));
  await expect(page.locator('[data-pane="tg"] [data-err="tg"]')).toHaveText('Вход отменён в Telegram.');
  await expect(page.locator('[data-tg-open]')).toBeHidden();
  await waitCall(request, tg, (c) => c.method === 'editMessageText' && String(c.params.text).includes('Вход отменён'));
  // новая ссылка
  await page.getByRole('button', { name: 'Получить новую ссылку' }).click();
  await expect(page.locator('[data-tg-open]')).toBeVisible();
  const again = await page.locator('[data-tg-open]').getAttribute('href');
  expect(again).not.toContain(token);
  // «Другой способ» — назад к кнопкам
  await page.getByRole('button', { name: 'Другой способ' }).click();
  await expect(page.locator('h1')).toHaveText('Вход в Охват');
});

test('бот: команды, чужой секрет вебхука, битые и чужие ссылки', async ({ request }) => {
  const tg = 9_000_000 + Math.floor(Math.random() * 1e6);
  // без секрета и с чужим — 401, до бота не доходит
  expect((await request.post('/api/telegram/webhook', { data: { update_id: 1, ...say(tg, '/start') } })).status()).toBe(401);
  expect((await hook(request, say(tg, '/start'), 'wrong')).status()).toBe(401);
  expect(await calls(request, tg)).toEqual([]);

  await hook(request, say(tg, '/start'));
  await waitCall(request, tg, (c) => String(c.params.text).includes('Это бот <b>Охват</b>'));
  await hook(request, say(tg, '/help'));
  await waitCall(request, tg, (c) => String(c.params.text).includes('Как войти на сайт'));
  await hook(request, say(tg, '/profile'));
  await waitCall(request, tg, (c) => String(c.params.text).includes('ещё не входили'));
  await hook(request, say(tg, '/stop'));
  await waitCall(request, tg, (c) => String(c.params.text).includes('Уведомления выключены'));
  await hook(request, say(tg, '/start'));
  await waitCall(request, tg, (c) => String(c.params.text).includes('снова включены'));
  await hook(request, say(tg, '/start login_' + 'x'.repeat(32)));
  await waitCall(request, tg, (c) => String(c.params.text).includes('Ссылка для входа устарела'));
  // повтор того же update_id от Telegram — без второго ответа
  const before = (await calls(request, tg)).length;
  const dup = { update_id: 999_000_000 + (tg % 1000), ...say(tg, '/help') };
  for (let i = 0; i < 2; i++) {
    await request.post('/api/telegram/webhook', { headers: { 'X-Telegram-Bot-Api-Secret-Token': SECRET }, data: dup });
  }
  await expect.poll(async () => (await calls(request, tg)).length).toBe(before + 1);
});

test('вход через бота: без CSRF — 403, без cookie браузера — ссылка устарела', async ({ request, playwright, baseURL }) => {
  expect((await request.post('/api/auth/telegram/bot/start', { data: { consent: true } })).status()).toBe(403);
  // чужой браузер (нет cookie ohvat_tglogin) вход не заберёт
  const other = await playwright.request.newContext({ baseURL });
  const oc = await (await other.get('/api/auth/config')).json();
  const st = await other.post('/api/auth/telegram/bot/status', { headers: { 'X-CSRF-Token': oc.csrfToken } });
  expect(st.status()).toBe(410);
  expect((await st.json()).error.code).toBe('login_expired');
  await other.dispose();
  // лимиты (30 запусков в час с адреса, токен-бакет 429) — в Go-тестах: здесь все браузеры с одного адреса
});
