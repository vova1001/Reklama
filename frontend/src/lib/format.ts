// Форматы чисел, денег и дат — одни для сервера (SSR) и браузера (предпросмотр).
import { FORMATS } from '../content/app';

const NUM = new Intl.NumberFormat('ru-RU');
const MONTHS = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря'];

/** 300000 коп. → «3 000 ₽». */
export const rub = (kop: number) => `${NUM.format(Math.round(kop / 100))} ₽`;
/** 12500 → «12 500». */
export const num = (n: number) => NUM.format(n);
/** «2026-11-20» → «до 20 ноября». */
export function until(iso: string | null | undefined): string {
  if (!iso) return '';
  const [y, m, d] = iso.split('-').map(Number);
  if (!y || !m || !d) return '';
  const year = new Date().getFullYear() === y ? '' : ` ${y}`;
  return `до ${d} ${MONTHS[m - 1]}${year}`;
}
/** Названия форматов через запятую. */
export const formatsTitle = (ids: string[]) => ids.map((f) => FORMATS.find((x) => x.id === f)?.title ?? f).join(', ');
/** Условия: «от 5 000 просмотров · от 300 лайков». */
export function goal(views: number, likes: number): string {
  const p: string[] = [];
  if (views) p.push(`от ${num(views)} просм.`);
  if (likes) p.push(`от ${num(likes)} лайков`);
  return p.join(' · ');
}
