// Вход и профиль: серверные запросы к API (сессия — cookie посетителя пробрасывается как есть). Контракт — backend/api/openapi.yaml.
import type { components } from './api.gen';

export type Me = components['schemas']['Me'];
export type User = components['schemas']['User'];
export type Mode = components['schemas']['Mode'];
export type AuthConfig = components['schemas']['AuthConfig'];
export type BusinessProfile = components['schemas']['BusinessProfile'];
export type CreatorProfile = components['schemas']['CreatorProfile'];

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
