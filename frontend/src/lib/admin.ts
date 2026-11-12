// Админка: серверные запросы к /api/admin/* от имени сотрудника (cookie и адрес посетителя пробрасываются) и
// справочники интерфейса. Права проверяет сервер на каждом запросе; здесь — только отображение.
import type { components } from './api.gen';
import { visitorHeaders } from './auth';

type S = components['schemas'];
export type AdminMe = S['AdminMe'];
export type Queue = S['Queue'];
export type QueueItem = S['QueueItem'];
export type ReviewItem = S['ReviewItem'];
export type ReviewKind = S['ReviewKind'];
export type Dashboard = S['Dashboard'];
export type AdminUser = S['AdminUser'];
export type UserCard = S['UserCard'];
export type AuditPage = S['AuditPage'];
export type AdminSettings = S['AdminSettings'];
export type ComplaintGroups = S['ComplaintGroups'];
export type ComplaintCase = S['ComplaintCase'];
export type Decision = S['Decision'];

const BACKEND_URL = process.env.BACKEND_URL ?? 'http://localhost:8080';

/** Ответ API: данные, или код ошибки (404 — нет/нет доступа, 403 — 2FA или права). */
export type Got<T> = { ok: true; data: T } | { ok: false; status: number; code: string };

export async function adminGet<T>(req: Request, path: string): Promise<Got<T>> {
  try {
    const res = await fetch(`${BACKEND_URL}${path}`, { headers: visitorHeaders(req), signal: AbortSignal.timeout(5000) });
    if (res.ok) return { ok: true, data: (await res.json()) as T };
    const j = (await res.json().catch(() => null)) as { error?: { code?: string } } | null;
    return { ok: false, status: res.status, code: j?.error?.code ?? '' };
  } catch (err) {
    console.warn(`API ${path} недоступно:`, (err as Error).message);
    return { ok: false, status: 503, code: 'unavailable' };
  }
}

/** Строка запроса из объекта (массивы — повторяющимися параметрами), пустые — пропускаются. */
export function qs(o: Record<string, string | string[] | number | undefined | null>): string {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(o)) for (const x of Array.isArray(v) ? v : [v]) if (x !== undefined && x !== null && x !== '') p.append(k, String(x));
  const s = p.toString();
  return s ? `?${s}` : '';
}

export const KIND_TITLE: Record<ReviewKind, string> = {
  task: 'Задание',
  task_revision: 'Правка задания',
  card: 'Анкета',
  card_revision: 'Правка анкеты',
  category: 'Своя сфера',
  format: 'Свой формат',
};

export const COMPLAINT_REASON: Record<string, string> = {
  spam: 'Спам или реклама',
  fraud: 'Мошенничество',
  offensive: 'Оскорбления',
  illegal: 'Запрещённое',
  fake: 'Фейк или накрутка',
  other: 'Другое',
};

export const SUBJECT_TITLE: Record<string, string> = { task: 'Задание', creator_card: 'Анкета', user: 'Профиль' };

export const ACTION_TITLE: Record<string, string> = { approve: 'Опубликовано', reject: 'Отклонено', rework: 'На доработку' };

export const ROLE_TITLE: Record<string, string> = { admin: 'Администратор', moderator: 'Модератор', '': 'Пользователь' };

export const PROVIDER_TITLE: Record<string, string> = { email: 'Почта', telegram: 'Telegram', google: 'Google' };

/** «5 мин», «3 ч», «2 дн» — сколько ждёт. */
export function waited(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return '—';
  const m = Math.max(0, Math.round((now - new Date(iso).getTime()) / 60000));
  if (m < 60) return `${m} мин`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h} ч`;
  return `${Math.floor(h / 24)} дн`;
}

/** Дата и время по Москве: «12.11, 14:05». */
export function when(iso: string | null | undefined): string {
  if (!iso) return '—';
  return new Date(iso).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow', day: '2-digit', month: '2-digit', year: '2-digit', hour: '2-digit', minute: '2-digit' });
}

/** Ссылка на карточку проверки. */
export const reviewHref = (t: string, id: string) => `/admin/review/${t}/${encodeURIComponent(id)}`;

export const can = (me: AdminMe, p: string) => me.permissions.includes(p);
