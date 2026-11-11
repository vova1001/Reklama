// Форматы чисел, денег и дат — одни для сервера (SSR) и браузера (предпросмотр).
import { FORMATS } from '../content/app';

const NUM = new Intl.NumberFormat('ru-RU');
const MONTHS = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря'];

/** 300000 коп. → «3 000 ₽». */
export const rub = (kop: number) => `${NUM.format(Math.round(kop / 100))} ₽`;
/** 12500 → «12 500». */
export const num = (n: number) => NUM.format(n);
/** «2026-11-20» (+ «18:30») → «до 20 ноября» («до 20 ноября, 18:30»). */
export function until(iso: string | null | undefined, time = ''): string {
  if (!iso) return '';
  const [y, m, d] = iso.split('-').map(Number);
  if (!y || !m || !d) return '';
  const year = new Date().getFullYear() === y ? '' : ` ${y}`;
  return `до ${d} ${MONTHS[m - 1]}${year}${time ? `, ${time}` : ''}`;
}
/** Названия форматов через запятую. */
export const formatsTitle = (ids: string[]) => ids.map((f) => FORMATS.find((x) => x.id === f)?.title ?? f).join(', ');
/** Условие выполнения: «от 5 000 просм.». */
export const goal = (views: number) => (views ? `от ${num(views)} просм.` : '');

// ===== подтверждённые соцсети (задача 12) =====
const DAY_MSK = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', timeZone: 'Europe/Moscow' });

/** «12 000 подписчиков» / «150 участников» (группа) / «подписчики скрыты». */
export function followersText(a: { followers: number | null; followersHidden: boolean; kind?: string; type?: string }): string {
  if (a.followersHidden || a.followers === null) return 'подписчики скрыты';
  const n = a.followers;
  const form = n % 10 === 1 && n % 100 !== 11 ? 0 : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 10 || n % 100 >= 20) ? 1 : 2;
  const group = (a.type ?? a.kind) === 'group';
  const words = group ? ['участник', 'участника', 'участников'] : ['подписчик', 'подписчика', 'подписчиков'];
  return `${NUM.format(n)} ${words[form]}`;
}

/** Вид аккаунта: канал, группа, профиль. */
export const kindTitle = (k: string) => (k === 'group' ? 'Группа' : k === 'channel' ? 'Канал' : 'Профиль');

/** «обновлено 10 ноября» (по Москве). */
export const updatedText = (iso: string | null | undefined) => (iso ? `обновлено ${DAY_MSK.format(new Date(iso))}` : '');

/** Строка под соцсетью в анкете: «Канал · обновлено 10 ноября». */
export const socialFacts = (p: { type: string; refreshedAt: string | null }) => [kindTitle(p.type), updatedText(p.refreshedAt)].filter(Boolean).join(' · ');
