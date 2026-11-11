// Динамические блоки: заголовок + пункты (абзацы, пункты списка или ссылки). Добавить / удалить / перетащить —
// и блоки, и пункты. Enter в пункте — новый пункт ниже, Backspace в пустом — удалить и вернуться к предыдущему.
// Удалённый блок можно вернуть из сообщения. Лимиты — как на сервере.
import type { components } from '../../lib/api.gen';
import { toast } from '../../lib/ui';
import { appear, autosize, h, ic, vanish, type IconName } from './dom';
import { sortable } from './sortable';

export type Block = components['schemas']['Block'];
type Kind = Block['kind'];

const MAX_BLOCKS = 8;
const MAX_ITEMS = 15;
const LIMIT: Record<Kind, number> = { text: 1000, list: 200, links: 300 };
const PLACEHOLDER: Record<Kind, string> = { text: 'Абзац текста', list: 'Пункт', links: 'https://…' };
const ADD: { kind: Kind; title: string; icon: IconName }[] = [
  { kind: 'list', title: 'Список', icon: 'list' },
  { kind: 'text', title: 'Текст', icon: 'text' },
  { kind: 'links', title: 'Ссылки', icon: 'link' },
];

export class BlocksEditor {
  private list: HTMLElement;
  private adds: HTMLElement;

  constructor(root: HTMLElement, blocks: Block[], private onChange: () => void) {
    this.list = h('div', { class: 'bks' });
    this.adds = h('div', { class: 'bk-adds' });
    for (const a of ADD) {
      const b = h('button', { class: 'btn pill-text', type: 'button', html: ic('plus', 18) }, a.title);
      b.addEventListener('click', () => {
        const el = this.block({ kind: a.kind, title: '', items: [] });
        this.list.append(el);
        appear(el);
        el.querySelector<HTMLInputElement>('.bk-title')!.focus();
        this.changed();
      });
      this.adds.append(b);
    }
    root.append(this.list, this.adds);
    blocks.forEach((b) => this.list.append(this.block(b)));
    sortable(this.list, {
      item: '.bk', handle: '.bk-grip', onChange: () => this.changed(),
      label: (el) => `Блок «${el.querySelector<HTMLInputElement>('.bk-title')!.value || 'без названия'}»`,
    });
    this.sync();
  }

  /** Блоки по порядку (пустые пункты отброшены). */
  value(): Block[] {
    return [...this.list.querySelectorAll<HTMLElement>('.bk')].map((el) => ({
      kind: el.dataset.kind as Kind,
      title: el.querySelector<HTMLInputElement>('.bk-title')!.value.trim(),
      items: [...el.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>('.bi-in')].map((i) => i.value.trim()).filter(Boolean),
    }));
  }

  /** Ошибки сервера: blocks.N — у блока N, blocks — общая. */
  showErrors(fields: Record<string, string>) {
    this.list.querySelectorAll<HTMLElement>('.bk').forEach((el, i) => {
      el.querySelector('.bk-err')!.textContent = fields[`blocks.${i}`] ?? '';
    });
  }

  focus() {
    (this.list.querySelector<HTMLElement>('.bi-add input') ?? this.adds.querySelector('button'))?.focus();
  }

  private changed() {
    this.sync();
    this.onChange();
  }

  private sync() {
    const n = this.list.querySelectorAll('.bk').length;
    this.adds.hidden = n >= MAX_BLOCKS;
    this.list.querySelectorAll<HTMLElement>('.bk').forEach((el) => {
      el.querySelector<HTMLElement>('.bi-add')!.hidden = el.querySelectorAll('.bi').length >= MAX_ITEMS;
    });
  }

  private block(b: Block): HTMLElement {
    const el = h('section', { class: 'bk', 'data-kind': b.kind });
    const title = h('input', { class: 'bk-title', type: 'text', maxlength: '60', placeholder: 'Заголовок блока', 'aria-label': 'Заголовок блока', value: b.title });
    title.value = b.title;
    const gripBtn = h('button', { class: 'bk-grip grip', type: 'button', 'aria-label': 'Переместить блок (стрелки вверх и вниз)', html: ic('grip', 20) });
    const del = h('button', { class: 'ibtn', type: 'button', 'aria-label': 'Удалить блок', html: ic('trash', 20) });
    const items = h('ol', { class: 'bis' });
    const addIn = h('input', { type: b.kind === 'links' ? 'url' : 'text', placeholder: `${PLACEHOLDER[b.kind]} — Enter`, maxlength: String(LIMIT[b.kind]), 'aria-label': 'Новый пункт', enterkeyhint: 'enter', inputmode: b.kind === 'links' ? 'url' : undefined });
    const add = h('div', { class: 'bi-add', html: ic('plus', 18) }, addIn);
    const err = h('p', { class: 'ferr bk-err', 'aria-live': 'polite' });
    el.append(h('div', { class: 'bk-h' }, gripBtn, title, del), items, add, err);
    b.items.forEach((t) => items.append(this.item(b.kind, t)));
    sortable(items, { item: '.bi', handle: '.grip', onChange: () => this.changed(), label: () => 'Пункт' });

    title.addEventListener('input', () => this.onChange());
    addIn.addEventListener('keydown', (e) => {
      if (e.key !== 'Enter' || e.isComposing) return;
      e.preventDefault();
      const v = addIn.value.trim();
      if (!v) return;
      const it = this.item(b.kind, v);
      items.append(it);
      appear(it);
      addIn.value = '';
      this.changed();
      if (items.children.length >= MAX_ITEMS) it.querySelector<HTMLElement>('.bi-in')?.focus();
    });
    // вставка нескольких строк — несколько пунктов
    addIn.addEventListener('paste', (e) => {
      const lines = (e.clipboardData?.getData('text') ?? '').split(/\r?\n/).map((s) => s.trim()).filter(Boolean);
      if (lines.length < 2) return;
      e.preventDefault();
      for (const l of lines.slice(0, MAX_ITEMS - items.children.length)) items.append(this.item(b.kind, l.slice(0, LIMIT[b.kind])));
      this.changed();
    });
    del.addEventListener('click', () => {
      const next = el.nextElementSibling;
      const snapshot = el;
      void vanish(el).then(() => {
        this.changed();
        toast('Блок удалён', () => {
          this.list.insertBefore(snapshot, next && next.parentElement === this.list ? next : null);
          snapshot.style.removeProperty('overflow');
          appear(snapshot);
          this.changed();
        });
      });
    });
    return el;
  }

  private item(kind: Kind, value: string): HTMLElement {
    const field =
      kind === 'text'
        ? h('textarea', { class: 'bi-in', rows: '1', maxlength: String(LIMIT.text), 'aria-label': 'Абзац' })
        : h('input', { class: 'bi-in', type: kind === 'links' ? 'url' : 'text', maxlength: String(LIMIT[kind]), 'aria-label': kind === 'links' ? 'Ссылка' : 'Пункт', inputmode: kind === 'links' ? 'url' : undefined });
    field.value = value;
    const count = kind === 'text' ? h('span', { class: 'bi-count', 'aria-live': 'polite' }) : null;
    const li = h('li', { class: 'bi' }, h('button', { class: 'grip', type: 'button', 'aria-label': 'Переместить пункт (стрелки)', html: ic('grip', 18) }), field, count, h('button', { class: 'ibtn bi-rm', type: 'button', 'aria-label': 'Удалить пункт', html: ic('x', 18) }));
    const upd = () => {
      if (field instanceof HTMLTextAreaElement) autosize(field);
      if (count) count.textContent = field.value.length > LIMIT.text * 0.8 ? `${field.value.length}/${LIMIT.text}` : '';
    };
    requestAnimationFrame(upd);
    field.addEventListener('input', () => {
      upd();
      this.onChange();
    });
    field.addEventListener('keydown', (e) => {
      const ke = e as KeyboardEvent;
      if (ke.key === 'Enter' && !ke.shiftKey && !ke.isComposing) {
        e.preventDefault();
        const ol = li.parentElement!;
        if (ol.children.length >= MAX_ITEMS) return;
        const n = this.item(kind, '');
        li.after(n);
        appear(n);
        n.querySelector<HTMLElement>('.bi-in')!.focus();
        this.changed();
      } else if (ke.key === 'Backspace' && field.value === '') {
        e.preventDefault();
        const prev = li.previousElementSibling?.querySelector<HTMLInputElement>('.bi-in');
        li.remove();
        (prev ?? li.closest('.bk')?.querySelector<HTMLInputElement>('.bi-add input'))?.focus();
        this.changed();
      }
    });
    li.querySelector('.bi-rm')!.addEventListener('click', () => {
      const bk = li.closest('.bk');
      void vanish(li).then(() => {
        bk?.querySelector<HTMLInputElement>('.bi-add input')?.focus();
        this.changed();
      });
    });
    return li;
  }
}
