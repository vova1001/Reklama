// «+ Своя / + Свой»: поле с подсказками (combobox) под пилюлями — общее для сфер, тем и форматов. Подсказки — с
// сервера (похожие, чтобы не плодить дубли), последней строкой — «Добавить «…»», если точного совпадения нет.
// Клавиатура: ↑/↓ — по подсказкам, Enter — выбрать, Escape — закрыть; ушли из пустого поля — закрывается.
import { appear, h, ic } from './dom';

export interface ComboItem {
  key: string;
  title: string;
  note?: string; // пометка справа («ваша», «вы уже использовали»)
}

export interface ComboOptions {
  placeholder: string;
  label: string; // для читалки
  maxlength: number;
  /** Похожие на запрос; строка — текст ошибки под полем. */
  search: (q: string) => Promise<ComboItem[] | string>;
  pick: (it: ComboItem) => void;
  /** «Добавить «q»»: true — добавлено (поле закрывается); ошибку пишет в err. */
  create: (q: string, err: HTMLElement) => Promise<boolean> | boolean;
}

const norm = (s: string) => s.toLowerCase().replace(/ё/g, 'е').replace(/[^\p{L}\p{N}]+/gu, ' ').trim();

export class Combo {
  private wrap: HTMLElement | null = null;

  constructor(private root: HTMLElement, private add: HTMLButtonElement, private o: ComboOptions) {
    add.addEventListener('click', () => this.open());
  }

  open() {
    if (this.wrap) return this.wrap.querySelector('input')!.focus();
    const { add, o } = this;
    const id = `cb-${Math.random().toString(36).slice(2, 8)}`;
    const input = h('input', {
      class: 'inp', type: 'text', maxlength: String(o.maxlength), placeholder: o.placeholder, role: 'combobox',
      'aria-expanded': 'false', 'aria-controls': id, 'aria-autocomplete': 'list', 'aria-label': o.label, autocomplete: 'off',
    });
    const box = h('ul', { class: 'cb-list', id, role: 'listbox', hidden: true });
    const err = h('p', { class: 'ferr', 'aria-live': 'polite' });
    const wrap = h('div', { class: 'cb' }, input, box, err);
    this.wrap = wrap;
    this.root.append(wrap);
    appear(wrap);
    add.setAttribute('aria-expanded', 'true');
    input.focus();

    let opts: { el: HTMLLIElement; run: () => void; isNew: boolean }[] = [];
    let active = -1;
    let timer = 0;
    let seq = 0;
    const close = () => {
      wrap.remove();
      this.wrap = null;
      add.setAttribute('aria-expanded', 'false');
      add.focus();
    };
    const setActive = (i: number) => {
      active = i;
      opts.forEach((x, j) => x.el.setAttribute('aria-selected', String(j === i)));
      input.setAttribute('aria-activedescendant', i >= 0 ? opts[i].el.id : '');
    };
    const run = (x: (typeof opts)[number]) => {
      if (x.isNew) {
        void Promise.resolve(o.create(input.value, err)).then((ok) => ok && close());
        return;
      }
      x.run();
      close();
    };
    const render = (items: ComboItem[], q: string) => {
      box.replaceChildren();
      opts = [];
      const exact = items.some((c) => norm(c.title) === norm(q));
      for (const c of items) {
        const el = h('li', { role: 'option', id: `${id}-${opts.length}`, 'aria-selected': 'false' }, c.title, c.note ? h('span', { class: 'chip-note' }, c.note) : null);
        opts.push({ el, run: () => o.pick(c), isNew: false });
        box.append(el);
      }
      if (!exact && q.trim().length >= 2) {
        const el = h('li', { role: 'option', id: `${id}-${opts.length}`, 'aria-selected': 'false', class: 'cb-new', html: ic('plus', 16) }, `Добавить «${q.trim()}»`);
        opts.push({ el, run: () => undefined, isNew: true });
        box.append(el);
      }
      opts.forEach((x) => x.el.addEventListener('pointerdown', (e) => {
        e.preventDefault();
        run(x);
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
        const r = await o.search(q);
        if (my !== seq) return; // пришёл ответ на старый ввод
        if (typeof r === 'string') return void (err.textContent = r);
        render(r, q);
      }, 150);
    });
    input.addEventListener('keydown', (e) => {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        if (opts.length) setActive((active + (e.key === 'ArrowDown' ? 1 : -1) + opts.length) % opts.length);
      } else if (e.key === 'Enter') {
        e.preventDefault();
        if (opts[active]) run(opts[active]);
      } else if (e.key === 'Escape') {
        e.preventDefault();
        close();
      }
    });
    input.addEventListener('blur', () => setTimeout(() => {
      if (this.wrap === wrap && !wrap.contains(document.activeElement) && !input.value.trim()) close();
    }, 150));
  }
}
