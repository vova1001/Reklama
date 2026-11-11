// Форматы задания: пилюли списка с отметкой + «+ Свой» (поле с подсказками: из списка, известные и ваши прошлые).
// Можно несколько — до max. Свой нормализуется как на сервере: без лишних пробелов, с заглавной; совпал с форматом
// списка (без учёта регистра) — отмечается он. Значение — slug из списка или название своего.
import { FORMATS } from '../../content/app';
import { api } from '../../lib/client';
import { guard, toast } from '../../lib/ui';
import { Combo } from './combo';
import { appear, h, ic } from './dom';

interface FormatItem { id: string; title: string; builtin: boolean; own: boolean }

export interface FormatOptions {
  max: number;
  onChange: () => void;
}

const norm = (s: string) => s.toLowerCase().replace(/ё/g, 'е').replace(/[^\p{L}\p{N}]+/gu, ' ').trim();
const builtin = (s: string) => FORMATS.find((f) => f.id === s || norm(f.title) === norm(s) || norm(f.id) === norm(s));

/** Свой формат к виду сервера; '' — пусто, строка с ошибкой — в err. */
export function normFormat(raw: string): { v: string; err: string } {
  const t = raw.trim().replace(/\s+/g, ' ');
  if (!t) return { v: '', err: '' };
  const b = builtin(t);
  if (b) return { v: b.id, err: '' };
  if (t.length < 2 || t.length > 30) return { v: '', err: 'От 2 до 30 символов' };
  if (!/\p{L}/u.test(t)) return { v: '', err: 'Нужны буквы' };
  if (/(https?:|www\.|:\/\/|@|\.(ru|com|рф|net|org|io|me)\b)/i.test(t)) return { v: '', err: 'Без ссылок' };
  return { v: t[0].toUpperCase() + t.slice(1), err: '' };
}

export class FormatPicker {
  private chips: HTMLElement;

  constructor(root: HTMLElement, selected: string[], private o: FormatOptions) {
    this.chips = h('div', { class: 'chips', role: 'group', 'aria-label': `Формат — можно несколько, до ${o.max}` });
    root.append(this.chips);
    const sel = new Set(selected);
    for (const f of FORMATS) this.chips.append(this.chip(f.id, f.title, sel.has(f.id), false));
    for (const s of selected) if (!builtin(s)) this.chips.append(this.chip(s, s, true, true));
    const add = h('button', { class: 'chip chip-add', type: 'button', 'aria-expanded': 'false', 'data-add-format': '', html: ic('plus', 18) }, 'Свой');
    this.chips.append(add);
    new Combo(root, add, {
      placeholder: 'Свой — например, «Stories» или «Обзор»', label: 'Свой формат', maxlength: 30,
      search: async (q) => {
        const r = await api<{ items: FormatItem[] }>('GET', `/api/me/formats/similar?q=${encodeURIComponent(q)}`);
        if (!r.ok) return guard(r.status) ? [] : r.error.message;
        return r.data.items.map((f) => ({ key: f.id, title: f.title, note: f.own ? 'ваш' : undefined }));
      },
      pick: (it) => this.pick(it.key),
      create: (q, err) => {
        const n = normFormat(q);
        if (n.err) {
          err.textContent = n.err;
          return false;
        }
        return n.v ? this.pick(n.v) : false;
      },
    });
  }

  value(): string[] {
    return [...this.chips.querySelectorAll<HTMLButtonElement>('[data-format][aria-pressed="true"]')].map((b) => b.dataset.format!);
  }

  private chip(id: string, title: string, on: boolean, own: boolean): HTMLButtonElement {
    const b = h('button', { class: `chip is-check ${own ? 'is-own' : ''}`, type: 'button', 'aria-pressed': String(on), 'data-format': id, html: ic('check', 16) }, title);
    b.addEventListener('click', () => this.toggle(b));
    return b;
  }

  private toggle(b: HTMLButtonElement, force?: boolean): boolean {
    const on = force ?? b.getAttribute('aria-pressed') !== 'true';
    if (on && b.getAttribute('aria-pressed') !== 'true' && this.value().length >= this.o.max) {
      toast(`Не больше ${this.o.max} форматов`);
      return false;
    }
    b.setAttribute('aria-pressed', String(on));
    this.o.onChange();
    return true;
  }

  /** Выбрать: формат списка или свой (новый — пилюлей перед «Свой»). */
  private pick(v: string): boolean {
    const b0 = builtin(v);
    const id = b0 ? b0.id : v;
    let b = [...this.chips.querySelectorAll<HTMLButtonElement>('[data-format]')].find((x) => norm(x.dataset.format!) === norm(id));
    if (!b) {
      b = this.chip(id, id, false, true);
      this.chips.insertBefore(b, this.chips.querySelector('.chip-add'));
      appear(b);
    }
    return b.getAttribute('aria-pressed') === 'true' || this.toggle(b, true);
  }
}
