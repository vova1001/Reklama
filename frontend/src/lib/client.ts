// Браузер: запросы к API с CSRF-токеном и помощники форм (вход и приложение). Без библиотек.
import type { components } from './api.gen';

export type ApiError = components['schemas']['Error']['error'];
export type Res<T> = { ok: true; status: number; data: T } | { ok: false; status: number; error: ApiError };
type Method = 'GET' | 'POST' | 'PUT' | 'PATCH';

const NET_ERROR: ApiError = { code: 'network', message: 'Нет связи с сервером. Проверьте интернет и попробуйте ещё раз.' };

// ===== API =====
let csrf = '';
async function csrfToken(force = false): Promise<string> {
  if (csrf && !force) return csrf;
  const res = await fetch('/api/auth/config', { credentials: 'same-origin', signal: AbortSignal.timeout(10_000) });
  if (!res.ok) throw new Error(String(res.status));
  csrf = ((await res.json()) as { csrfToken: string }).csrfToken;
  return csrf;
}

/** Запрос к API: ответ или ошибка в едином формате; устаревший CSRF-токен обновляется и запрос повторяется один раз. */
export async function api<T>(method: Method, path: string, body?: unknown, retried = false, timeout = 15_000): Promise<Res<T>> {
  try {
    const headers: Record<string, string> = {};
    if (method !== 'GET') {
      headers['Content-Type'] = 'application/json';
      headers['X-CSRF-Token'] = await csrfToken();
    }
    const res = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(timeout),
    });
    if (res.status === 204) return { ok: true, status: 204, data: undefined as T };
    const json = await res.json().catch(() => null);
    if (res.ok) return { ok: true, status: res.status, data: json as T };
    const error: ApiError = json?.error ?? { code: 'internal', message: 'Что-то пошло не так. Попробуйте позже.' };
    const ra = Number(res.headers.get('Retry-After'));
    if (ra > 0 && !error.retryAfter) error.retryAfter = ra;
    if (res.status === 403 && error.code === 'csrf' && !retried) {
      await csrfToken(true);
      return api<T>(method, path, body, true, timeout);
    }
    return { ok: false, status: res.status, error };
  } catch {
    return { ok: false, status: 0, error: NET_ERROR };
  }
}

// ===== помощники интерфейса =====
export const $ = <T extends Element = HTMLElement>(sel: string, root: ParentNode = document) => root.querySelector<T>(sel);
export const $$ = <T extends Element = HTMLElement>(sel: string, root: ParentNode = document) => [...root.querySelectorAll<T>(sel)];

/** Ошибка поля: текст в [data-err=field], поле [data-input=field] помечается aria-invalid. */
export function setErr(root: ParentNode, field: string, msg: string) {
  const el = $(`[data-err="${field}"]`, root);
  if (el) el.textContent = msg;
  $(`[data-input="${field}"]`, root)?.setAttribute('aria-invalid', msg ? 'true' : 'false');
}
export function clearErrs(root: ParentNode) {
  $$('[data-err]', root).forEach((e) => (e.textContent = ''));
  $$('[aria-invalid]', root).forEach((e) => e.setAttribute('aria-invalid', 'false'));
}
/** Ошибки полей от сервера — рядом с полями; без полей — общая строка формы. */
export function showFields(root: ParentNode, e: ApiError) {
  const fields = Object.entries(e.fields ?? {});
  for (const [k, v] of fields) setErr(root, k, v);
  if (!fields.length) setErr(root, 'form', e.message);
}
export function busy(btn: HTMLButtonElement | null, on: boolean) {
  if (!btn) return;
  btn.disabled = on;
  btn.classList.toggle('is-busy', on);
  btn.setAttribute('aria-busy', String(on));
}
export const mmss = (s: number) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;

/** Обратный отсчёт: tick(осталось) каждую секунду, done() в конце. Возвращает отмену. */
export function countdown(sec: number, tick: (left: number) => void, done: () => void): () => void {
  const end = Date.now() + sec * 1000;
  let t = 0;
  const step = () => {
    const left = Math.max(0, Math.ceil((end - Date.now()) / 1000));
    if (left <= 0) return done();
    tick(left);
    t = window.setTimeout(step, 250);
  };
  step();
  return () => clearTimeout(t);
}
