// Вход и профиль: серверные запросы к API (сессия — cookie посетителя пробрасывается как есть). Контракт — backend/api/openapi.yaml.
import type { components } from './api.gen';

export type Me = components['schemas']['Me'];
export type User = components['schemas']['User'];
export type Mode = components['schemas']['Mode'];
export type AuthConfig = components['schemas']['AuthConfig'];
export type BusinessProfile = components['schemas']['BusinessProfile'];
export type CreatorProfile = components['schemas']['CreatorProfile'];
export type Task = components['schemas']['Task'];
export type Card = components['schemas']['Card'];
export type CategoryItem = components['schemas']['CategoryItem'];
export type Feed = components['schemas']['Feed'];

const BACKEND_URL = process.env.BACKEND_URL ?? 'http://localhost:8080';
export const SESSION_COOKIE = 'ohvat_session';

/** Заголовки для запроса к API от имени посетителя: его cookie и адрес (лимиты API считаются по адресу посетителя,
 *  а не сервера сайта — иначе все посетители делили бы один лимит). */
export function visitorHeaders(req: Request): Record<string, string> {
  const h: Record<string, string> = { cookie: req.headers.get('cookie') ?? '' };
  const ip = req.headers.get('x-real-ip');
  if (ip) h['x-real-ip'] = ip;
  return h;
}

async function getJSON<T>(path: string, headers: Record<string, string>): Promise<T | null> {
  try {
    const res = await fetch(`${BACKEND_URL}${path}`, { headers, signal: AbortSignal.timeout(3000) });
    if (!res.ok) return null;
    return (await res.json()) as T;
  } catch (err) {
    console.warn(`API ${path} недоступно:`, (err as Error).message);
    return null;
  }
}

/** Кто вошёл: null — гость или сессия истекла; API недоступно — тоже гость (страница всё равно открывается). */
export function fetchMe(req: Request): Promise<Me | null> {
  return getJSON<Me>('/api/me', visitorHeaders(req));
}

/** Профили режимов (null — не заполнен или API недоступно). */
export async function fetchProfiles(req: Request): Promise<{ business: BusinessProfile | null; creator: CreatorProfile | null }> {
  const h = visitorHeaders(req);
  const [b, c] = await Promise.all([
    getJSON<{ profile: BusinessProfile | null }>('/api/me/profile/business', h),
    getJSON<{ profile: CreatorProfile | null }>('/api/me/profile/creator', h),
  ]);
  return { business: b?.profile ?? null, creator: c?.profile ?? null };
}

export type CardFeed = components['schemas']['CardFeed'];
/** Отбор ленты: сферы (темы) и форматы (только задания) — любая из выбранных; город; курсор. */
export interface FeedQuery { category?: string[]; city?: string; format?: string[]; cursor?: string }

const qs = (o: Record<string, string | string[] | undefined>) => {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(o)) for (const x of Array.isArray(v) ? v : [v]) if (x) p.append(k, x);
  const s = p.toString();
  return s ? `?${s}` : '';
};

/** Лента опубликованных заданий (null — API недоступно или неверный отбор). */
export function fetchFeed(req: Request, q: FeedQuery = {}): Promise<Feed | null> {
  return getJSON<Feed>(`/api/tasks${qs({ category: q.category, city: q.city, format: q.format, cursor: q.cursor })}`, visitorHeaders(req));
}

/** Лента опубликованных анкет авторов (null — API недоступно или неверный отбор). */
export function fetchCardFeed(req: Request, q: FeedQuery = {}): Promise<CardFeed | null> {
  return getJSON<CardFeed>(`/api/cards${qs({ topic: q.category, city: q.city, cursor: q.cursor })}`, visitorHeaders(req));
}

/** Опубликованное задание (null — нет или не опубликовано). */
export function fetchPublicTask(req: Request, id: number): Promise<Task | null> {
  return getJSON<Task>(`/api/tasks/${id}`, visitorHeaders(req));
}

/** Опубликованная анкета автора (null — нет или не опубликована). */
export function fetchPublicCard(req: Request, id: number): Promise<Card | null> {
  return getJSON<Card>(`/api/cards/${id}`, visitorHeaders(req));
}

/** Мои задания (null — API недоступно). */
export async function fetchMyTasks(req: Request): Promise<Task[] | null> {
  const r = await getJSON<{ tasks: Task[] }>('/api/me/tasks', visitorHeaders(req));
  return r?.tasks ?? null;
}

/** Моё задание; null — нет, чужое или API недоступно. */
export function fetchTask(req: Request, id: number): Promise<Task | null> {
  return getJSON<Task>(`/api/me/tasks/${id}`, visitorHeaders(req));
}

/** Моя анкета автора: undefined — API недоступно, null — ещё нет. */
export async function fetchCard(req: Request): Promise<Card | null | undefined> {
  const r = await getJSON<{ card: Card | null }>('/api/me/card', visitorHeaders(req));
  return r === null ? undefined : r.card;
}

/** Общие категории и свои (пусто — API недоступно). */
export async function fetchCategories(req: Request): Promise<{ common: CategoryItem[]; own: CategoryItem[] }> {
  return (await getJSON<{ common: CategoryItem[]; own: CategoryItem[] }>('/api/me/categories', visitorHeaders(req))) ?? { common: [], own: [] };
}

let cfgCache: { at: number; data: AuthConfig } | null = null;

/** Какие способы входа включены (кэш 30 с). Токен CSRF отсюда не используется — браузер берёт свой. */
export async function fetchAuthConfig(): Promise<AuthConfig> {
  if (cfgCache && Date.now() - cfgCache.at < 30_000) return cfgCache.data;
  try {
    const res = await fetch(`${BACKEND_URL}/api/auth/config`, { signal: AbortSignal.timeout(3000) });
    if (!res.ok) throw new Error(String(res.status));
    const data = (await res.json()) as AuthConfig;
    cfgCache = { at: Date.now(), data };
    return data;
  } catch (err) {
    console.warn('API /api/auth/config недоступно:', (err as Error).message);
    return cfgCache?.data ?? { email: false, google: false, telegram: null, csrfToken: '' };
  }
}

export const MODE_TITLE: Record<Mode, string> = { business: 'Бизнес', creator: 'Автор' };

/** Имя для шапки: первое слово; пусто — «Профиль». */
export function shortName(u: User): string {
  return u.name.trim().split(/\s+/)[0] || 'Профиль';
}

/** Буква для аватара без фото. */
export function initial(u: User): string {
  return (u.name.trim()[0] ?? u.email?.[0] ?? 'О').toUpperCase();
}

// ===== страница бизнеса и профиль автора =====
export type ProfilePage = components['schemas']['Page'];
export type PageBlock = components['schemas']['PageBlock'];
export type PublicPage = components['schemas']['PublicPage'];
export type PageKind = components['schemas']['PageKind'];

/** Моя страница: undefined — API недоступно; page null — ещё нет (suggestedSlug — адрес новой). */
export async function fetchMyPage(req: Request, kind: PageKind, name = ''): Promise<{ page: ProfilePage | null; suggestedSlug?: string } | undefined> {
  const r = await getJSON<{ page: ProfilePage | null; suggestedSlug?: string }>(`/api/me/pages/${kind}${qs({ name })}`, visitorHeaders(req));
  return r ?? undefined;
}

/** Опубликованная страница: null — нет (или API недоступно — тогда 503 решает страница по status). */
export async function fetchPublicPage(req: Request, kind: PageKind, slug: string): Promise<{ status: number; data: PublicPage | null }> {
  try {
    const res = await fetch(`${BACKEND_URL}/api/pages/${kind}/${encodeURIComponent(slug)}`, { headers: visitorHeaders(req), signal: AbortSignal.timeout(3000) });
    if (!res.ok) return { status: res.status, data: null };
    return { status: 200, data: (await res.json()) as PublicPage };
  } catch (err) {
    console.warn('API страницы недоступно:', (err as Error).message);
    return { status: 503, data: null };
  }
}
