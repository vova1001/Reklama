// Браузер: общие помощники приложения — сообщение (с «Вернуть»), свой диалог подтверждения, выход при 401, числа.
import { $ } from './client';

let toastTimer = 0;
/** Сообщение внизу; undo — кнопка «Вернуть» (6 с). */
export function toast(msg: string, undo?: () => void) {
  const el = $('[data-toast]');
  if (!el) return;
  $('[data-toast-text]', el)!.textContent = msg;
  const act = $<HTMLButtonElement>('[data-toast-act]', el)!;
  act.hidden = !undo;
  act.onclick = undo
    ? () => {
        el.classList.remove('is-on');
        undo();
      }
    : null;
  el.classList.add('is-on');
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => el.classList.remove('is-on'), undo ? 6000 : 3200);
}

/** Не вошёл (сессия истекла или вышли на другом устройстве) — на вход. */
export function guard(status: number): boolean {
  if (status === 401) {
    location.assign('/login');
    return true;
  }
  return false;
}

/** Свой диалог подтверждения вместо confirm(). */
export function confirmDialog(title: string, text: string, ok: string): Promise<boolean> {
  const dlg = $<HTMLDialogElement>('[data-confirm]');
  if (!dlg || typeof dlg.showModal !== 'function') return Promise.resolve(true);
  $('[data-confirm-title]', dlg)!.textContent = title;
  $('[data-confirm-text]', dlg)!.textContent = text;
  $('[data-confirm-ok]', dlg)!.textContent = ok;
  dlg.returnValue = '';
  dlg.showModal();
  return new Promise((resolve) => dlg.addEventListener('close', () => resolve(dlg.returnValue === 'ok'), { once: true }));
}

const NUM = new Intl.NumberFormat('ru-RU');
/** 10000 → «10 000» (в полях — обычный пробел: удобнее править и копировать). */
export const fmtNum = (n: number) => NUM.format(n).replace(/[\u00a0\u202f]/g, ' ');
/** «10 000», «10к» — только цифры → число (пусто — 0). */
export const parseNum = (s: string) => Number(s.replace(/\D/g, '').slice(0, 12)) || 0;
