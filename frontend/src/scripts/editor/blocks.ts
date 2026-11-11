// Динамические блоки: заголовок (правится в строке) + пункты (абзацы, пункты списка или ссылки).
// Пункт — всегда поле: Enter — новый пункт (текст после курсора переносится), Backspace в пустом — удалить,
// вставка нескольких строк — несколько пунктов, Alt+↑/↓ — переставить (в заголовке — весь блок), перетаскивание
// за ручку. Шаблоны блоков — одним нажатием. Удалённый блок можно вернуть из сообщения. Лимиты — как на сервере.
import type { components } from '../../lib/api.gen';
import type { BlockKind } from '../../content/app';
import { toast } from '../../lib/ui';
import { appear, autosize, h, ic, vanish } from './dom';
import { sortable } from './sortable';

export type Block = components['schemas']['Block'];
type Kind = BlockKind;
type Field = HTMLInputElement | HTMLTextAreaElement;

const MAX_BLOCKS = 8;
const MAX_ITEMS = 15;
const LIMIT: Record<Kind, number> = { text: 1000, list: 200, links: 300 };
const PLACEHOLDER: Record<Kind, string> = { text: 'Текст — Enter, новый абзац', list: 'Пункт — Enter, следующий', links: 'Ссылка, например vk.com/clip-1' };
const ADD_ITEM: Record<Kind, string> = { text: 'Абзац', list: 'Пункт', links: 'Ссылка' };

export class BlocksEditor {
  private list: HTMLElement;
  private tpl: HTMLElement;

  constructor(
    root: HTMLElement,
    blocks: Block[],
    private templates: { title: string; kind: Kind }[],
    private onChange: () => void,
  ) {
    this.list = h('div', { class: 'bks' });
    this.tpl = h('div', { class: 'bk-tpl', role: 'group', 'aria-label': 'Добавить блок' });
    root.append(this.list, this.tpl);
    blocks.forEach((b) => this.list.append(this.block(b)));
    sortable(this.list, {
      item: '.bk', handle: '.bk-grip', onChange: () => this.changed(),
      label: (el) => `Блок «${$title(el).value || 'без заголовка'}»`,
    });
    this.sync();
  }

  /** Блоки по порядку; пустые пункты отброшены (номер блока — как у ошибок сервера blocks.N). */
  value(): Block[] {
    return this.blocks().map((el) => ({
      kind: el.dataset.kind as Kind,
      title: $title(el).value.trim(),
      items: fields(el).map((i) => i.value.trim()).filter(Boolean),
    }));
  }

  /** Ошибки сервера: blocks.N — у блока N. */
  showErrors(f: Record<string, string>) {
    this.blocks().forEach((el, i) => {
      el.querySelector('.bk-err')!.textContent = f[`blocks.${i}`] ?? '';
      el.classList.toggle('is-bad', !!f[`blocks.${i}`]);
    });
  }

  /** Первое поле для ввода (к нему ведёт «Осталось: описание»). */
  first(): HTMLElement | null {
    return this.list.querySelector<HTMLElement>('.bi-in') ?? this.tpl.querySelector('button');
  }

  private blocks() {
    return [...this.list.querySelectorAll<HTMLElement>(':scope > .bk')];
  }

  private changed() {
    this.sync();
    this.onChange();
  }

  /** Шаблоны: только те, которых ещё нет; «Свой блок» — всегда (до 8 блоков). */
  private sync() {
    const have = new Set(this.blocks().map((el) => $title(el).value.trim().toLowerCase()));
    const full = this.blocks().length >= MAX_BLOCKS;
    this.tpl.replaceChildren();
    if (full) return;
    for (const t of [...this.templates.filter((t) => !have.has(t.title.toLowerCase())), { title: '', kind: 'list' as Kind }]) {
      const b = h('button', { class: 'chip sm', type: 'button', html: ic('plus', 16) }, t.title || 'Свой блок');
      b.addEventListener('click', () => {
        const el = this.block({ kind: t.kind, title: t.title, items: [] });
        this.list.append(el);
        appear(el);
        (t.title ? fields(el)[0] : $title(el)).focus();
        this.changed();
      });
      this.tpl.append(b);
    }
  }

  private block(b: Block): HTMLElement {
    const kind = b.kind as Kind;
    const el = h('section', { class: 'bk', 'data-kind': kind });
    const title = h('input', { class: 'bk-title', type: 'text', maxlength: '60', placeholder: 'Заголовок блока', 'aria-label': 'Заголовок блока' });
    title.value = b.title;
    const grip = h('button', { class: 'bk-grip grip', type: 'button', 'aria-label': 'Переместить блок: стрелки или Alt+↑/↓', html: ic('grip', 20) });
    const del = h('button', { class: 'ibtn bk-del', type: 'button', 'aria-label': 'Удалить блок', html: ic('trash', 18) });
    const items = h('ol', { class: 'bis' });
    const more = h('button', { class: 'bi-more', type: 'button', html: ic('plus', 16) }, ADD_ITEM[kind]);
    const err = h('p', { class: 'ferr bk-err', 'aria-live': 'polite' });
    el.append(h('div', { class: 'bk-h' }, grip, title, del), items, more, err);
    (b.items.length ? b.items : ['']).forEach((t) => items.append(this.item(kind, t)));
    sortable(items, { item: '.bi', handle: '.grip', onChange: () => this.changed(), label: () => 'Пункт' });

    title.addEventListener('input', () => this.onChange());
    title.addEventListener('change', () => this.sync());
    title.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        fields(el)[0]?.focus();
      } else if (e.altKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
        e.preventDefault();
        if (move(el, e.key === 'ArrowUp')) {
          title.focus();
          this.changed();
        }
      }
    });
    more.addEventListener('click', () => {
      const last = fields(el).at(-1);
      if (last && !last.value.trim()) return last.focus();
      this.insertAfter(kind, items.lastElementChild as HTMLElement | null, items, '');
    });
    del.addEventListener('click', () => {
      const next = el.nextElementSibling;
      void vanish(el).then(() => {
        this.changed();
        toast('Блок удалён', () => {
          this.list.insertBefore(el, next && next.parentElement === this.list ? next : null);
          el.style.removeProperty('overflow');
          appear(el);
          this.changed();
        });
      });
    });
    return el;
  }

  /** Новый пункт после li (null — в начало); фокус в него. */
  private insertAfter(kind: Kind, li: HTMLElement | null, items: HTMLElement, value: string, caret = 0): Field | null {
    if (items.children.length >= MAX_ITEMS) {
      toast(`Не больше ${MAX_ITEMS} пунктов в блоке`);
      return null;
    }
    const n = this.item(kind, value);
    if (li) li.after(n);
    else items.prepend(n);
    appear(n);
    const f = n.querySelector<Field>('.bi-in')!;
    f.focus();
    f.setSelectionRange(caret, caret);
    this.changed();
    return f;
  }

  private item(kind: Kind, value: string): HTMLElement {
    const field: Field =
      kind === 'text'
        ? h('textarea', { class: 'bi-in', rows: '1', maxlength: String(LIMIT.text), 'aria-label': 'Абзац', placeholder: PLACEHOLDER.text })
        : h('input', { class: 'bi-in', type: 'text', maxlength: String(LIMIT[kind]), 'aria-label': kind === 'links' ? 'Ссылка' : 'Пункт', placeholder: PLACEHOLDER[kind], inputmode: kind === 'links' ? 'url' : undefined, enterkeyhint: 'next' });
    field.value = value;
    const count = kind === 'text' ? h('span', { class: 'bi-count', 'aria-live': 'polite' }) : null;
    const rm = h('button', { class: 'ibtn bi-rm', type: 'button', 'aria-label': 'Удалить пункт', html: ic('x', 16) });
    const li = h('li', { class: 'bi' }, h('button', { class: 'grip', type: 'button', 'aria-label': 'Переместить пункт: стрелки или Alt+↑/↓', html: ic('grip', 16) }), field, count, rm);
    const upd = () => {
      if (field instanceof HTMLTextAreaElement) autosize(field);
      if (count) count.textContent = field.value.length > LIMIT.text * 0.8 ? `${field.value.length}/${LIMIT.text}` : '';
    };
    requestAnimationFrame(upd);
    const items = () => li.parentElement!;

    field.addEventListener('input', () => {
      upd();
      this.onChange();
    });
    field.addEventListener('keydown', (e) => {
      const ke = e as KeyboardEvent;
      if (ke.isComposing) return;
      if (ke.key === 'Enter' && !ke.shiftKey) {
        // новый пункт ниже; текст после курсора — в него
        e.preventDefault();
        const at = field.selectionStart ?? field.value.length;
        const tail = field.value.slice(field.selectionEnd ?? at);
        if (this.insertAfter(kind, li, items(), tail.trimStart()) && tail) {
          field.value = field.value.slice(0, at).trimEnd();
          upd();
        }
      } else if (ke.key === 'Backspace' && field.value === '' && items().children.length > 1) {
        e.preventDefault();
        const prev = (li.previousElementSibling ?? li.nextElementSibling)?.querySelector<Field>('.bi-in');
        li.remove();
        if (prev) {
          prev.focus();
          prev.setSelectionRange(prev.value.length, prev.value.length);
        }
        this.changed();
      } else if (ke.altKey && (ke.key === 'ArrowUp' || ke.key === 'ArrowDown')) {
        e.preventDefault();
        if (move(li, ke.key === 'ArrowUp')) {
          field.focus();
          this.changed();
        }
      }
    });
    // вставка нескольких строк — несколько пунктов (первая строка — в текущий)
    field.addEventListener('paste', (ev) => {
      const e = ev as ClipboardEvent;
      const lines = (e.clipboardData?.getData('text') ?? '').split(/\r?\n/).map((s: string) => s.replace(/^\s*(?:[-•*·–—]|\d+[.)])\s+/, '').trim()).filter(Boolean);
      if (lines.length < 2) return;
      e.preventDefault();
      const at = field.selectionStart ?? field.value.length;
      const tail = field.value.slice(field.selectionEnd ?? at);
      field.value = (field.value.slice(0, at) + lines[0]).slice(0, LIMIT[kind]);
      upd();
      let cur: HTMLElement = li;
      const room = MAX_ITEMS - items().children.length;
      const rest = lines.slice(1, 1 + room);
      rest.forEach((l: string, i: number) => {
        const n = this.item(kind, (i === rest.length - 1 ? l + tail : l).slice(0, LIMIT[kind]));
        cur.after(n);
        cur = n;
      });
      if (lines.length - 1 > room) toast(`Не больше ${MAX_ITEMS} пунктов в блоке`);
      cur.querySelector<Field>('.bi-in')!.focus();
      this.changed();
    });
    rm.addEventListener('click', () => {
      if (items().children.length === 1) {
        field.value = '';
        upd();
        field.focus();
        return this.changed();
      }
      const near = (li.nextElementSibling ?? li.previousElementSibling)?.querySelector<Field>('.bi-in');
      void vanish(li).then(() => {
        near?.focus();
        this.changed();
      });
    });
    return li;
  }
}

const $title = (el: HTMLElement) => el.querySelector<HTMLInputElement>('.bk-title')!;
const fields = (el: HTMLElement) => [...el.querySelectorAll<Field>('.bi-in')];

/** Сдвинуть элемент на место вверх или вниз среди соседей того же вида; true — сдвинулся. */
function move(el: HTMLElement, up: boolean): boolean {
  const sib = (up ? el.previousElementSibling : el.nextElementSibling) as HTMLElement | null;
  if (!sib || !sib.classList.contains(el.classList[0])) return false;
  sib.insertAdjacentElement(up ? 'beforebegin' : 'afterend', el);
  live(`Место ${[...el.parentElement!.children].indexOf(el) + 1} из ${el.parentElement!.children.length}`);
  return true;
}

let liveEl: HTMLElement | null = null;
function live(msg: string) {
  if (!liveEl) {
    liveEl = h('p', { class: 'sr-only', 'aria-live': 'polite' });
    document.body.append(liveEl);
  }
  liveEl.textContent = msg;
}
