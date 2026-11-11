// Сферы и темы: общий справочник пилюлями + своя. Своя — поле с подсказками похожих (общих и своих), чтобы не
// плодить дубли; «Добавить …» создаёт её на сервере («на проверке» — видна только вам). multi — несколько
// (до max, своих — до maxCustom), иначе одна.
import type { components } from '../../lib/api.gen';
import { api } from '../../lib/client';
import { guard, toast } from '../../lib/ui';
import { appear, h, ic } from './dom';

type Item = components['schemas']['CategoryItem'];

export interface PickerOptions {
  multi: boolean;
  max: number;
  maxCustom: number;
  label: string; // для читалки: «Сфера», «Темы»
  onChange: () => void;
}

const norm = (s: string) => s.toLowerCase().replace(/ё/g, 'е').replace(/[^\p{L}\p{N}]+/gu, ' ').trim();

export class CategoryPicker {
  private chips: HTMLElement;
  private own = new Map<string, Item>();
  private combo: HTMLElement | null = null;

  constructor(private root: HTMLElement, common: Item[], own: Item[], selected: string[], private o: PickerOptions) {
    this.chips = h('div', { class: 'chips', role: 'group', 'aria-label': o.label });
    root.append(this.chips);
    const sel = new Set(selected);
    for (const c of common) this.chips.append(this.chip(c, sel.has(c.slug)));
    for (const c of own) {
      this.own.set(c.slug, c);
      // свои показываем, если выбраны или ещё на проверке (отклонённые — только если выбраны)
      if (sel.has(c.slug) || c.status === 'pending_review') this.chips.append(this.chip(c, sel.has(c.slug)));
    }
    const add = h('button', { class: 'chip chip-add', type: 'button', 'aria-pressed': 'false', html: ic('plus', 18) }, 'Своя');
    add.addEventListener('click', () => this.openCombo(add));
    this.chips.append(add);
  }

  value(): string[] {
    return [...this.chips.querySelectorAll<HTMLButtonElement>('[data-slug][aria-pressed="true"]')].map((b) => b.dataset.slug!);
  }

  /** Названия выбранных (для предпросмотра). */
  titles(): string[] {
    return [...this.chips.querySelectorAll<HTMLButtonElement>('[data-slug][aria-pressed="true"]')].map((b) => b.dataset.title!);
  }

  private chip(c: Item, on: boolean): HTMLButtonElement {
    const b = h('button', { class: `chip ${c.own ? 'is-own' : ''}`, type: 'button', 'aria-pressed': String(on), 'data-slug': c.slug, 'data-title': c.title, html: c.own ? ic('tag', 16) : undefined }, c.title);
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

  // ===== своя: поле с подсказками (combobox) =====
  private openCombo(add: HTMLButtonElement) {
    if (this.combo) return this.combo.querySelector('input')!.focus();
    const id = `cb-${Math.random().toString(36).slice(2, 8)}`;
    const input = h('input', {
      class: 'inp', type: 'text', maxlength: '40', placeholder: 'Своя — например, «Настольные игры»', role: 'combobox',
      'aria-expanded': 'false', 'aria-controls': id, 'aria-autocomplete': 'list', 'aria-label': 'Своя категория', autocomplete: 'off',
    });
    const box = h('ul', { class: 'cb-list', id, role: 'listbox', hidden: true });
    const err = h('p', { class: 'ferr', 'aria-live': 'polite' });
    const wrap = h('div', { class: 'cb' }, input, box, err);
    this.combo = wrap;
    this.root.append(wrap);
    appear(wrap);
    add.setAttribute('aria-pressed', 'true');
    input.focus();

    let opts: { el: HTMLLIElement; run: () => void }[] = [];
    let active = -1;
    let timer = 0;
    let seq = 0;
    const close = () => {
      wrap.remove();
      this.combo = null;
      add.setAttribute('aria-pressed', 'false');
      add.focus();
    };
    const setActive = (i: number) => {
      active = i;
      opts.forEach((o, j) => o.el.setAttribute('aria-selected', String(j === i)));
      input.setAttribute('aria-activedescendant', i >= 0 ? opts[i].el.id : '');
    };
    const render = (items: Item[], q: string) => {
      box.replaceChildren();
      opts = [];
      const exact = items.find((c) => norm(c.title) === norm(q));
      for (const c of items) {
        const el = h('li', { role: 'option', id: `${id}-${opts.length}`, 'aria-selected': 'false' }, c.title, c.own ? h('span', { class: 'chip-note' }, 'ваша') : null);
        opts.push({ el, run: () => this.pick(c) });
        box.append(el);
      }
      if (!exact && q.trim().length >= 2) {
        const el = h('li', { role: 'option', id: `${id}-${opts.length}`, 'aria-selected': 'false', class: 'cb-new', html: ic('plus', 16) }, `Добавить «${q.trim()}»`);
        opts.push({ el, run: () => void this.create(q, err).then((ok) => ok && close()) });
        box.append(el);
      }
      opts.forEach((o) => o.el.addEventListener('pointerdown', (e) => {
        e.preventDefault();
        o.run();
        if (!o.el.classList.contains('cb-new')) close();
      }));
      box.hidden = opts.length === 0;
      input.setAttribute('aria-expanded', String(!box.hidden));
      setActive(opts.length ? 0 : -1);
    };
    input.addEventListener('input', () => {
      err.textContent = '';
      clearTimeout(timer);
      const q = input.value;
      if (q.trim().length < 2) return render([], q);
      timer = window.setTimeout(async () => {
        const my = ++seq;
        const r = await api<{ items: Item[] }>('GET', `/api/me/categories/similar?q=${encodeURIComponent(q)}`);
        if (my !== seq) return; // пришёл ответ на старый ввод
        if (!r.ok) return void (guard(r.status) || (err.textContent = r.error.message));
        render(r.data.items, q);
      }, 150);
    });
    input.addEventListener('keydown', (e) => {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        if (opts.length) setActive((active + (e.key === 'ArrowDown' ? 1 : -1) + opts.length) % opts.length);
      } else if (e.key === 'Enter') {
        e.preventDefault();
        const o = opts[active];
        if (!o) return;
        o.run();
        if (!o.el.classList.contains('cb-new')) close();
      } else if (e.key === 'Escape') {
        e.preventDefault();
        close();
      }
    });
    input.addEventListener('blur', () => setTimeout(() => {
      if (this.combo === wrap && !wrap.contains(document.activeElement) && !input.value.trim()) close();
    }, 150));
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
