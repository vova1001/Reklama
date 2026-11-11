// Подтверждение соцсетей в e2e: действия человека в Telegram — вебхуком (как их присылает Telegram), канал или группа —
// в подставном Bot API (POST /__chats). Настоящий Telegram и Google не вызываются (стек make e2e-up).
import { expect, type APIRequestContext, type Page } from '@playwright/test';

const SECRET = process.env.E2E_TG_SECRET ?? 'e2e-webhook-secret';
export const FAKE_TG = process.env.E2E_FAKE_TG ?? 'http://localhost:8081';
let updateId = (Date.now() % 1e9) + 5e8;

export async function botOn(request: APIRequestContext) {
  const cfg = await (await request.get('/api/auth/config')).json();
  return !!cfg.telegram?.bot;
}

export function hook(request: APIRequestContext, update: Record<string, unknown>) {
  return request.post('/api/telegram/webhook', { headers: { 'X-Telegram-Bot-Api-Secret-Token': SECRET }, data: { update_id: ++updateId, ...update } });
}

const from = (id: number) => ({ id, is_bot: false, first_name: 'Алина', username: `a${id}` });
export const say = (id: number, text: string) => ({ message: { message_id: 1, from: from(id), chat: { id, type: 'private' }, text } });
export const tap = (id: number, data: string) => ({
  callback_query: { id: `cb${id}${updateId}`, from: from(id), data, message: { message_id: 7, chat: { id, type: 'private' } } },
});

export type Chat = { id: number; type: 'channel' | 'group' | 'supergroup'; title: string; username?: string; members: number };

/** Канал или группа в подставном Bot API: tgId — создатель (или admins). */
export async function setChat(request: APIRequestContext, c: Chat, admins: { id: number; status: string }[]) {
  expect((await request.post(`${FAKE_TG}/__chats`, { data: { ...c, admins } })).status()).toBe(204);
}

/** Бота добавили в канал / группу (или убрали) — my_chat_member от того, кто это сделал. */
export const member = (tgId: number, c: Chat, status: string) => ({
  my_chat_member: { chat: { id: c.id, type: c.type, title: c.title, username: c.username }, from: from(tgId), date: 1, new_chat_member: { status, user: { id: 1, is_bot: true } } },
});

/** «Подключить» Telegram в анкете → токен из ссылки на бота. */
export async function startTelegram(page: Page): Promise<string> {
  const row = page.locator('[data-platform="telegram"]');
  await row.locator('[data-act]').click();
  const go = row.locator('.soc-go');
  await expect(go).toBeVisible();
  const href = (await go.getAttribute('href')) ?? '';
  const m = href.match(/^https:\/\/t\.me\/ohvat_test_bot\?start=ch_([A-Za-z0-9_-]{43})$/);
  expect(m, `ссылка на бота: ${href}`).toBeTruthy();
  expect(href.split('start=')[1].length).toBeLessThanOrEqual(64);
  return m![1];
}

/** Весь путь: «Подключить» → бот /start → «Привязать» → бота добавили в канал → «Подтверждено» в анкете. */
export async function connectTelegram(page: Page, request: APIRequestContext, tgId: number, chat: Chat) {
  const tok = await startTelegram(page);
  expect((await hook(request, say(tgId, `/start ch_${tok}`))).status()).toBe(200);
  expect((await hook(request, tap(tgId, `sl:${tok}`))).status()).toBe(200);
  await setChat(request, chat, [{ id: tgId, status: 'creator' }]);
  expect((await hook(request, member(tgId, chat, 'administrator'))).status()).toBe(200);
  await expect(page.locator(`[data-platform="telegram"] .soc-a`, { hasText: chat.title }).locator('.soc-badge')).toHaveText('Подтверждено', { timeout: 15_000 });
}

export const tgUser = () => 8_000_000 + Math.floor(Math.random() * 1e6);
export const chatId = () => -(1_000_000_000_000 + Math.floor(Math.random() * 1e9));
