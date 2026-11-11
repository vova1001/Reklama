// Мелкие помощники разметки для редакторов: элементы, иконки (только нужные — не весь набор), анимации.
import grip from '../../icons/grip-vertical.svg?raw';
import x from '../../icons/x.svg?raw';
import plus from '../../icons/plus.svg?raw';
import trash from '../../icons/trash-2.svg?raw';
import imagePlus from '../../icons/image-plus.svg?raw';
import tag from '../../icons/tag.svg?raw';
import alert from '../../icons/circle-alert.svg?raw';
import crop from '../../icons/crop.svg?raw';
import list from '../../icons/list.svg?raw';
import text from '../../icons/text.svg?raw';
import link from '../../icons/link.svg?raw';
import wallet from '../../icons/wallet.svg?raw';
import eye from '../../icons/eye.svg?raw';
import video from '../../icons/video.svg?raw';
import calendar from '../../icons/calendar.svg?raw';
import users from '../../icons/users.svg?raw';
import camera from '../../icons/camera.svg?raw';
import check from '../../icons/check.svg?raw';
import left from '../../icons/arrow-left.svg?raw';
import right from '../../icons/arrow-right.svg?raw';
import clock from '../../icons/clock.svg?raw';
import send from '../../icons/send.svg?raw';
import youtube from '../../icons/youtube.svg?raw';
import instagram from '../../icons/instagram.svg?raw';
import atSign from '../../icons/at-sign.svg?raw';
import refresh from '../../icons/refresh-cw.svg?raw';
import badge from '../../icons/badge-check.svg?raw';

const RAW = {
  grip, x, plus, trash, imagePlus, tag, alert, crop, list, text, link, wallet, eye, video, calendar, users, camera, check, left, right, clock,
  send, youtube, instagram, atSign, refresh, badge,
};
export type IconName = keyof typeof RAW;

/** Иконка Lucide (линия 1.5, как на сервере) строкой SVG. */
export function ic(name: IconName, size = 20): string {
  return RAW[name]
    .replace(/<!--.*?-->/gs, '')
    .replace(/<svg[^>]*>/s, (m) => m.replace(/\s(width|height|class)="[^"]*"/g, ''))
    .replace('stroke-width="2"', 'stroke-width="1.5"')
    .replace('<svg', `<svg width="${size}" height="${size}" class="ic" aria-hidden="true" focusable="false"`);
}

/** Элемент: h('button', { class: 'x', type: 'button' }, 'текст' | узлы). Текст — только textContent (без HTML). */
export function h<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Record<string, string | boolean | undefined> = {},
  ...kids: (Node | string | null | undefined)[]
): HTMLElementTagNameMap[K] {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v === undefined || v === false) continue;
    if (k === 'html') el.innerHTML = String(v); // только для наших иконок
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const k of kids) if (k !== null && k !== undefined) el.append(k);
  return el;
}

const reduce = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

/** Появление: короткое (160 мс). */
export function appear(el: HTMLElement) {
  if (reduce()) return;
  el.animate([{ opacity: 0, transform: 'translateY(-4px)' }, { opacity: 1, transform: 'none' }], { duration: 160, easing: 'cubic-bezier(.2,.7,.2,1)' });
}

/** Исчезновение со сворачиванием высоты, затем удаление. */
export function vanish(el: HTMLElement): Promise<void> {
  if (reduce()) {
    el.remove();
    return Promise.resolve();
  }
  const hgt = el.offsetHeight;
  const a = el.animate(
    [{ opacity: 1, height: `${hgt}px` }, { opacity: 0, height: '0px', marginTop: '0px', marginBottom: '0px', paddingTop: '0px', paddingBottom: '0px' }],
    { duration: 160, easing: 'ease' },
  );
  el.style.overflow = 'hidden';
  return a.finished.then(() => el.remove()).catch(() => el.remove());
}

/** Высота многострочного поля — по содержимому. */
export function autosize(t: HTMLTextAreaElement) {
  t.style.height = 'auto';
  t.style.height = `${t.scrollHeight}px`;
}
