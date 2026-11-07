// Данные главной из API бэкенда (контракт — backend/api/openapi.yaml, типы — api.gen.ts).
import type { components } from './api.gen';

export type Home = components['schemas']['Home'];
export type Plan = components['schemas']['Plan'];
export type Category = components['schemas']['Category'];
export type Question = components['schemas']['Question'];

const BACKEND_URL = process.env.BACKEND_URL ?? 'http://localhost:8080';
const TTL_MS = 30_000;

let cache: { at: number; data: Home } | null = null;

/** Данные главной: свежие — из API (кэш 30 секунд); API недоступно — последние полученные. */
export async function getHome(): Promise<Home> {
  if (cache && Date.now() - cache.at < TTL_MS) return cache.data;
  try {
    const res = await fetch(`${BACKEND_URL}/api/home`, { signal: AbortSignal.timeout(3000) });
    if (!res.ok) throw new Error(`API /api/home: ${res.status}`);
    const data = (await res.json()) as Home;
    cache = { at: Date.now(), data };
    return data;
  } catch (err) {
    if (cache) {
      console.warn('API недоступно, показываю последние данные:', (err as Error).message);
      return cache.data;
    }
    throw err;
  }
}
