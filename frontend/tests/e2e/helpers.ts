// Общее для e2e приложения: вход по почте с кодом из журнала бэкенда (режим разработки, MAIL_PROVIDER=log).
import { expect, type Page } from '@playwright/test';
import { execSync } from 'node:child_process';

const repo = new URL('../../../', import.meta.url).pathname;

function codeFor(email: string): string {
  const logs = execSync('docker compose logs backend --no-log-prefix --since 10m', { cwd: repo, encoding: 'utf8', maxBuffer: 32 << 20 });
  const line = logs.split('\n').filter((l) => l.includes(`"to":"${email}"`) && l.includes('Код для входа')).at(-1);
  const m = line?.match(/Код для входа в Охват: (\d{6})/);
  if (!m) throw new Error(`нет кода в журнале для ${email}`);
  return m[1];
}

export const uniq = (p: string, name: string) => `e2e-app-${name}-${p}-${Date.now()}-${Math.floor(Math.random() * 1e6)}@example.com`;

export function jsErrors(page: Page) {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  return errors;
}

export async function login(page: Page, email: string) {
  await page.goto('/login');
  await page.getByRole('button', { name: 'Войти по почте' }).click();
  await page.getByLabel('Почта').fill(email);
  await page.getByRole('button', { name: 'Получить код' }).click();
  await expect(page.getByRole('heading', { name: 'Введите код' })).toBeVisible();
  await page.locator('[data-digit]').first().fill(codeFor(email));
  await page.waitForURL('**/app');
}

