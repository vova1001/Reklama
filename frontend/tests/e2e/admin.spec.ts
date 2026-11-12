// Админка насквозь: вход администратора из ADMIN_EMAILS с настройкой 2FA (TOTP), опубликовать и отклонить
// задание из очереди (горячие клавиши, решение применяется через 10 с), жалоба пользователя → блокировка владельца.
// Задания на проверку кладутся в базу напрямую (в e2e режим проверки — auto, общий для всех тестов).
import { expect, test, type Page } from '@playwright/test';
import { createHmac } from 'node:crypto';
import { execSync } from 'node:child_process';
import { jsErrors, login, uniq } from './helpers';

const repo = new URL('../../../', import.meta.url).pathname;
const sql = (q: string) => execSync(`docker compose exec -T db psql -U ohvat -d ohvat -tAc "${q}"`, { cwd: repo, encoding: 'utf8' }).trim();

function totp(secretB32: string, at = Date.now()): string {
  const alpha = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = '';
  for (const c of secretB32.replace(/\s/g, '')) bits += alpha.indexOf(c).toString(2).padStart(5, '0');
  const key = Buffer.from(bits.match(/.{8}/g)!.map((b) => parseInt(b, 2)));
  const msg = Buffer.alloc(8);
  msg.writeBigUInt64BE(BigInt(Math.floor(at / 1000 / 30)));
  const h = createHmac('sha1', key).update(msg).digest();
  const o = h[h.length - 1] & 15;
  return String((h.readUInt32BE(o) & 0x7fffffff) % 1_000_000).padStart(6, '0');
}

/** Запрос к API из страницы (с CSRF), как это делает приложение. */
async function call<T = unknown>(page: Page, method: string, path: string, body?: unknown): Promise<{ status: number; data: T }> {
  return page.evaluate(
    async ([method, path, body]) => {
      const cfg = await (await fetch('/api/auth/config')).json();
      const res = await fetch(path as string, { method: method as string, headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': cfg.csrfToken }, body: body ? JSON.stringify(body) : undefined });
      return { status: res.status, data: res.status === 204 ? null : await res.json().catch(() => null) };
    },
    [method, path, body] as const,
  ) as Promise<{ status: number; data: T }>;
}

/** Задание бизнеса, сразу «на проверке» (в базе). */
async function pendingTask(page: Page, title: string): Promise<number> {
  const by = new Date(Date.now() + 5 * 86400_000).toISOString().slice(0, 10);
  const t = await call<{ id: number }>(page, 'POST', '/api/me/tasks', {
    title, city: 'Казань', nationwide: false, categories: ['food'], formats: ['reels'], access: 'open', minViews: 1000, rewardKopecks: 50000,
    publishBy: by, publishTime: '', blocks: [{ kind: 'text', title: 'Описание', items: ['Снять обзор нового меню'] }], cover: '', photos: [],
  });
  expect(t.status).toBe(201);
  sql(`UPDATE tasks SET status = 'pending_review', submitted_at = now() - interval '1 hour', published_at = NULL WHERE id = ${t.data.id}`);
  return t.data.id;
}

test.describe.configure({ mode: 'serial' });

test('администратор: 2FA, опубликовать и отклонить из очереди, жалоба и блокировка', async ({ browser }, info) => {
  test.setTimeout(150_000);
  const adminEmail = `e2e-admin-${info.project.name}@example.com`;
  sql(`DELETE FROM staff_totp WHERE user_id IN (SELECT id FROM users WHERE email = '${adminEmail}'); DELETE FROM review_leases WHERE moderator_id IN (SELECT id FROM users WHERE email = '${adminEmail}'); DELETE FROM login_codes WHERE email = '${adminEmail}'`);

  // бизнес: два задания на проверке
  const bizCtx = await browser.newContext();
  const biz = await bizCtx.newPage();
  await login(biz, uniq(info.project.name, 'biz'));
  const okTitle = `E2E опубликовать ${Date.now()}`;
  const badTitle = `E2E отклонить ${Date.now()}`;
  const okID = await pendingTask(biz, okTitle);
  const badID = await pendingTask(biz, badTitle);

  // обычному пользователю админки нет
  await biz.goto('/admin');
  await expect(biz.getByText('404').first()).toBeVisible();
  expect((await call(biz, 'GET', '/api/admin/queue')).status).toBe(404);

  // администратор: вход → настройка 2FA
  const admCtx = await browser.newContext();
  const adm = await admCtx.newPage();
  const errors = jsErrors(adm);
  await login(adm, adminEmail);
  await adm.goto('/admin');
  await adm.waitForURL('**/admin/2fa');
  expect((await call(adm, 'GET', '/api/admin/queue')).status).toBe(403);
  await adm.getByRole('button', { name: 'Начать настройку' }).click();
  await expect(adm.locator('[data-tfa-key]')).toHaveText(/[A-Z2-7]{4}/);
  const key = (await adm.locator('[data-tfa-key]').textContent())!;
  await expect(adm.locator('[data-tfa-qr]')).toBeVisible();
  await adm.getByLabel('2. Код из приложения').fill(totp(key));
  await adm.getByRole('button', { name: 'Включить' }).click();
  await expect(adm.locator('[data-tfa-codes] span')).toHaveCount(10);
  await adm.getByRole('link', { name: /в админку/ }).click();
  await expect(adm.getByRole('heading', { name: 'Сводка' })).toBeVisible();

  // очередь: оба задания, поиск
  await adm.goto(`/admin/queue?q=${encodeURIComponent(okTitle)}`);
  await expect(adm.locator('[data-queue] tbody tr')).toHaveCount(1);
  await adm.getByRole('link', { name: okTitle }).click();
  await expect(adm.locator('[data-lease]')).toContainText('В работе у вас');
  await expect(adm.getByRole('heading', { name: okTitle })).toBeVisible();
  // A — опубликовать; сразу открывается следующий, внизу — отмена 10 с
  await adm.keyboard.press('a');
  await expect(adm.locator('[data-toast]')).toContainText('Опубликовано');
  await expect(adm.locator('[data-toast-act]')).toContainText('Отменить');

  // отклонить: открыть второе, R → шаблон причины → «Отклонить»
  await adm.goto(`/admin/review/task/${badID}`);
  await expect(adm.locator('[data-lease]')).toContainText('В работе у вас');
  await adm.keyboard.press('r');
  await adm.locator('[data-tpl]').first().click();
  await adm.locator('#f-reject').getByRole('button', { name: 'Отклонить' }).click();
  await expect(adm.locator('[data-toast]')).toContainText('Отклонено');

  // через 10 с решения применены: одно опубликовано, у другого — причина
  await expect.poll(async () => (await call(biz, 'GET', `/api/tasks/${okID}`)).status, { timeout: 25_000, intervals: [1000] }).toBe(200);
  await expect.poll(async () => (await call<{ status: string; rejectReason: string }>(biz, 'GET', `/api/me/tasks/${badID}`)).data.status, { timeout: 25_000, intervals: [1000] }).toBe('rejected');
  await biz.goto(`/app/tasks/${badID}`);
  await expect(biz.getByText('Отклонено.')).toBeVisible();

  // автор жалуется на опубликованное задание
  const autCtx = await browser.newContext();
  const aut = await autCtx.newPage();
  await login(aut, uniq(info.project.name, 'aut'));
  await aut.goto(`/app/task/${okID}`);
  await aut.getByRole('button', { name: 'Пожаловаться' }).click();
  await aut.getByRole('radio', { name: 'Мошенничество' }).click();
  await aut.getByLabel(/Комментарий/).fill('Просят оплату вперёд');
  await aut.getByRole('button', { name: 'Отправить' }).click();
  await expect(aut.locator('[data-toast]')).toContainText('жалоба отправлена');

  // администратор: жалоба → заблокировать владельца
  await adm.goto('/admin/complaints?subject=task');
  await adm.locator(`[data-c-item="task:${okID}"] a`).click();
  await adm.getByLabel('Причина (увидит владелец)').fill('Мошенничество с оплатой подтвердилось');
  await adm.getByRole('button', { name: 'Заблокировать владельца' }).click();
  await adm.locator('[data-confirm-ok]').click();
  await adm.waitForURL('**/admin/complaints');

  // заблокированный вышел везде, задание пропало из ленты
  expect((await call(biz, 'GET', '/api/me')).status).toBe(401);
  expect((await call(aut, 'GET', `/api/tasks/${okID}`)).status).toBe(404);

  // журнал: блокировка записана
  await adm.goto('/admin/audit?action=user.block');
  await expect(adm.locator('[data-audit] tbody tr').first()).toContainText('Мошенничество с оплатой');
  expect(errors).toEqual([]);
  await Promise.all([bizCtx.close(), admCtx.close(), autCtx.close()]);
});
