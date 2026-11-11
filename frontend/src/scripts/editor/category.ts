// Сферы и темы: общий справочник пилюлями с отметкой + своя. Своя — поле с подсказками похожих (общих и своих),
// чтобы не плодить дубли; «Добавить …» создаёт её на сервере («на проверке» — видна только вам). multi — несколько
// (до max, своих — до maxCustom), иначе одна.
import type { components } from '../../lib/api.gen';
import { api } from '../../lib/client';
import { guard, toast } from '../../lib/ui';
import { Combo } from './combo';
import { appear, h, ic } from './dom';

type Item = components['schemas']['CategoryItem'];

export interface PickerOptions {
  multi: boolean;
  max: number;
  maxCustom: number;
  label: string; // для читалки: «Сфера», «Темы»
  addLabel: string; // «Своя», «Своя тема»
  onChange: () => void;
}

export class CategoryPicker {
  private chips: HTMLElement;
  private own = new Map<string, Item>();

  constructor(root: HTMLElement, common: Item[], own: Item[], selected: string[], private o: PickerOptions) {
    this.chips = h('div', { class: 'chips', role: 'group', 'aria-label': `${o.label}${o.multi ? ` — можно несколько, до ${o.max}` : ''}` });
    root.append(this.chips);
    const sel = new Set(selected);
    for (const c of common) this.chips.append(this.chip(c, sel.has(c.slug)));
    for (const c of own) {
      this.own.set(c.slug, c);
      // свои показываем, если выбраны или ещё на проверке (отклонённые — только если выбраны)
      if (sel.has(c.slug) || c.status === 'pending_review') this.chips.append(this.chip(c, sel.has(c.slug)));
    }
    const add = h('button', { class: 'chip chip-add', type: 'button', 'aria-expanded': 'false', html: ic('plus', 18) }, o.addLabel);
    this.chips.append(add);
    const found = new Map<string, Item>(); // подсказки с сервера — чтобы выбрать по ключу
    new Combo(root, add, {
      placeholder: 'Своя — например, «Настольные игры»', label: 'Своя категория', maxlength: 40,
      search: async (q) => {
        const r = await api<{ items: Item[] }>('GET', `/api/me/categories/similar?q=${encodeURIComponent(q)}`);
        if (!r.ok) return guard(r.status) ? [] : r.error.message;
        r.data.items.forEach((c) => found.set(c.slug, c));
        return r.data.items.map((c) => ({ key: c.slug, title: c.title, note: c.own ? 'ваша' : undefined }));
      },
      pick: (it) => {
        const c = found.get(it.key);
        if (c) this.pick(c);
      },
      create: (q, err) => this.create(q, err),
    });
  }

  value(): string[] {
    return [...this.chips.querySelectorAll<HTMLButtonElement>('[data-slug][aria-pressed="true"]')].map((b) => b.dataset.slug!);
  }

  /** Названия выбранных (для предпросмотра). */
  titles(): string[] {
    return [...this.chips.querySelectorAll<HTMLButtonElement>('[data-slug][aria-pressed="true"]')].map((b) => b.dataset.title!);
  }

  private chip(c: Item, on: boolean): HTMLButtonElement {
    const b = h('button', { class: `chip is-check ${c.own ? 'is-own' : ''}`, type: 'button', 'aria-pressed': String(on), 'data-slug': c.slug, 'data-title': c.title, html: ic('check', 16) }, c.title);
    if (c.own) b.append(h('span', { class: 'chip-note' }, c.status === 'rejected' ? 'отклонена' : 'на проверке'));
    b.addEventListener('click', () => this.toggle(b));
    return b;
  }

  private toggle(b: HTMLButtonElement, force?: boolean) {
    const on = force ?? b.getAttribute('aria-pressed') !== 'true';
    if (on) {
      const cur = this.value();
      if (!this.o.multi) this.chips.querySelectorAll('[data-slug]').forEach((x) => x.setAttribute('aria-pressed', 'false'));
      else if (cur.length >= this.o.max) return toast(`Не больше ${this.o.max}`);
      else if (b.classList.contains('is-own') && cur.filter((s) => this.own.has(s)).length >= this.o.maxCustom) {
        return toast(`Своих — не больше ${this.o.maxCustom}`);
      }
    }
    b.setAttribute('aria-pressed', String(on));
    this.o.onChange();
  }

  /** Выбрать существующую (общую или свою). */
  private pick(c: Item) {
    let b = this.chips.querySelector<HTMLButtonElement>(`[data-slug="${CSS.escape(c.slug)}"]`);
    if (!b) {
      if (c.own) this.own.set(c.slug, c);
      b = this.chip(c, false);
      this.chips.insertBefore(b, this.chips.querySelector('.chip-add'));
      appear(b);
    }
    if (b.getAttribute('aria-pressed') !== 'true') this.toggle(b, true);
  }

  private async create(q: string, err: HTMLElement): Promise<boolean> {
    const title = q.trim().replace(/\s+/g, ' ');
    if (title.length < 2 || title.length > 40) {
      err.textContent = 'От 2 до 40 символов';
      return false;
    }
    const r = await api<{ category: Item; created: boolean }>('POST', '/api/me/categories', { title });
    if (!r.ok) {
      if (!guard(r.status)) err.textContent = r.error.fields?.title ?? r.error.message;
      return false;
    }
    this.pick(r.data.category);
    if (r.data.created) toast('Своя категория — на проверке, вам видна сразу');
    return true;
  }
}
