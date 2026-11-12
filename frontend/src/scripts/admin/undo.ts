// Отмена решения в течение 10 с: после решения карточка сразу сменяется следующей, а внизу — полоса
// «Опубликовано · Отменить (9)». Отмена возвращает к элементу (он снова в работе у модератора).
import { $, api } from '../../lib/client';
import { guard, toast } from '../../lib/ui';

interface Pending { id: number; action: string; title: string; applyAt: string; back: string }
const KEY = 'adm-undo';
const WORD: Record<string, string> = { approve: 'Опубликовано', reject: 'Отклонено', rework: 'На доработку' };

export function rememberUndo(p: Pending) {
  sessionStorage.setItem(KEY, JSON.stringify(p));
}

function show() {
  let p: Pending;
  try {
    p = JSON.parse(sessionStorage.getItem(KEY) ?? 'null');
  } catch {
    return;
  }
  if (!p) return;
  const left = () => Math.ceil((new Date(p.applyAt).getTime() - Date.now()) / 1000);
  if (left() <= 0) return sessionStorage.removeItem(KEY);
  const el = $('[data-toast]');
  if (!el) return;
  const text = $('[data-toast-text]', el)!;
  const act = $<HTMLButtonElement>('[data-toast-act]', el)!;
  const label = $('span', act);
  el.dataset.undo = String(p.id);
  act.hidden = false;
  el.classList.add('is-on');
  const tick = () => {
    const s = left();
    if (s <= 0) {
      el.classList.remove('is-on');
      sessionStorage.removeItem(KEY);
      return;
    }
    text.textContent = `${WORD[p.action] ?? 'Решено'}${p.title ? ` · «${p.title.slice(0, 40)}»` : ''}`;
    if (label) label.textContent = `Отменить (${s})`;
    timer = window.setTimeout(tick, 250);
  };
  let timer = 0;
  tick();
  act.onclick = async () => {
    clearTimeout(timer);
    act.disabled = true;
    const r = await api('POST', `/api/admin/decisions/${p.id}/undo`);
    act.disabled = false;
    sessionStorage.removeItem(KEY);
    el.classList.remove('is-on');
    if (!r.ok) return guard(r.status) || toast(r.error.message);
    location.assign(p.back);
  };
}
show();
