// Вход: серверные запросы к API (сессия — cookie посетителя пробрасывается как есть). Контракт — backend/api/openapi.yaml.
import type { components } from './api.gen';

export type Me = components['schemas']['Me'];
export type User = components['schemas']['User'];
export type AuthConfig = components['schemas']['AuthConfig'];
export type Role = components['schemas']['Role'];

const BACKEND_URL = process.env.BACKEND_URL ?? 'http://localhost:8080';
export const SESSION_COOKIE = 'ohvat_session';

/** Кто вошёл: null — гость или сессия истекла; API недоступно — тоже гость (страница всё равно открывается). */
export async function fetchMe(cookie: string): Promise<Me | null> {
  try {
    const res = await fetch(`${BACKEND_URL}/api/me`, { headers: { cookie }, signal: AbortSignal.timeout(3000) });
    if (!res.ok) return null;
    return (await res.json()) as Me;
  } catch (err) {
    console.warn('API /api/me недоступно:', (err as Error).message);
    return null;
  }
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

export const ROLE_TITLE: Record<Role, string> = { business: 'Бизнес', creator: 'Автор' };

/** Имя для шапки: первое слово; пусто — «Кабинет». */
export function shortName(u: User): string {
  return u.name.trim().split(/\s+/)[0] || 'Кабинет';
}

/** Буква для аватара без фото. */
export function initial(u: User): string {
  return (u.name.trim()[0] ?? u.email?.[0] ?? 'О').toUpperCase();
}
