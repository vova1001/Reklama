// Срок публикации: пилюли «3 дня / Неделя / 2 недели / Месяц / + Свой». «Свой» раскрывает свой календарь (без
// системного) и время по желанию; без «Своего» дата отдельным полем не показывается. Значение — в скрытом поле
// publishBy (ДД.ММ.ГГГГ) и поле publishTime (ЧЧ:ММ), как и раньше: черновик и проверка их читают по имени.
import { $, $$ } from '../../lib/client';
import { appear, h, ic } from './dom';

const MONTHS = ['Январь', 'Февраль', 'Март', 'Апрель', 'Май', 'Июнь', 'Июль', 'Август', 'Сентябрь', 'Октябрь', 'Ноябрь', 'Декабрь'];
const MONTHS_GEN = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря'];
const DAYS = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс'];

const pad = (n: number) => String(n).padStart(2, '0');
/** Дата по местному времени, ГГГГ-ММ-ДД. */
export const isoOf = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
/** Сегодня + n дней, ГГГГ-ММ-ДД. */
export function isoDay(n: number): string {
  const t = new Date();
  t.setDate(t.getDate() + n);
  return isoOf(t);
}
const parseIso = (s: string) => {
  const [y, m, d] = s.split('-').map(Number);
  return new Date(y, m - 1, d);
};
const dotted = (iso: string) => iso.split('-').reverse().join('.');
const fromDotted = (v: string) => {
  const m = v.match(/^(\d{2})\.(\d{2})\.(\d{4})$/);
  return m ? `${m[3]}-${m[2]}-${m[1]}` : '';
};

/** Свой календарь месяца: дни — кнопки (стрелки — по дням и неделям, PageUp/PageDown — месяцы), вне min…max — недоступны. */
class Calendar {
  private month: Date;
  private head = h('p', { class: 'cal-m', 'aria-live': 'polite' });
  private grid = h('div', { class: 'cal-g', role: 'group' });
  private focusIso = '';

  constructor(root: HTMLElement, private min: string, private max: string, private value: string, private onPick: (iso: string) => void) {
    const start = parseIso(value || min);
    this.month = new Date(start.getFullYear(), start.getMonth(), 1);
    const prev = h('button', { class: 'ibtn', type: 'button', 'aria-label': 'Предыдущий месяц', html: ic('left', 18) });
    const next = h('button', { class: 'ibtn', type: 'button', 'aria-label': 'Следующий месяц', html: ic('right', 18) });
    prev.addEventListener('click', () => this.shift(-1));
    next.addEventListener('click', () => this.shift(1));
    const dows = h('div', { class: 'cal-w', 'aria-hidden': 'true' }, ...DAYS.map((d) => h('span', {}, d)));
    root.append(h('div', { class: 'cal' }, h('div', { class: 'cal-h' }, prev, this.head, next), dows, this.grid));
    this.grid.addEventListener('keydown', (e) => this.key(e));
    this.render();
  }

  set(iso: string) {
    this.value = iso;
    this.render();
  }

  private shift(by: number, focus = '') {
    this.month = new Date(this.month.getFullYear(), this.month.getMonth() + by, 1);
    this.render(focus);
  }

  private render(focus = '') {
    const y = this.month.getFullYear();
    const m = this.month.getMonth();
    this.head.textContent = `${MONTHS[m]} ${y}`;
    this.grid.setAttribute('aria-label', `${MONTHS[m]} ${y}`);
    const first = (new Date(y, m, 1).getDay() + 6) % 7; // пн = 0
    const days = new Date(y, m + 1, 0).getDate();
    const inMonth = (iso: string) => iso.startsWith(`${y}-${pad(m + 1)}`);
    // фокус — выбранный день, иначе первый доступный в месяце
    const ok = (iso: string) => iso >= this.min && iso <= this.max;
    let tab = focus || (inMonth(this.value) ? this.value : '');
    if (!tab || !inMonth(tab) || !ok(tab)) {
      tab = '';
      for (let d = 1; d <= days && !tab; d++) if (ok(`${y}-${pad(m + 1)}-${pad(d)}`)) tab = `${y}-${pad(m + 1)}-${pad(d)}`;
    }
    this.focusIso = tab;
    const cells: HTMLElement[] = [];
    for (let i = 0; i < first; i++) cells.push(h('span', { class: 'cal-e' }));
    for (let d = 1; d <= days; d++) {
      const iso = `${y}-${pad(m + 1)}-${pad(d)}`;
      const b = h('button', {
        type: 'button', class: 'cal-d', 'data-iso': iso, 'aria-label': `${d} ${MONTHS_GEN[m]} ${y}`,
        'aria-pressed': String(iso === this.value), disabled: !ok(iso), tabindex: iso === tab ? '0' : '-1',
        'aria-current': iso === isoDay(0) ? 'date' : undefined,
      }, String(d));
      b.addEventListener('click', () => {
        this.value = iso;
        this.render(iso);
        this.onPick(iso);
      });
      cells.push(b);
    }
    this.grid.replaceChildren(...cells);
    if (focus) $<HTMLButtonElement>(`[data-iso="${focus}"]`, this.grid)?.focus();
  }

  private key(e: KeyboardEvent) {
    const step: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 };
    if (!(e.key in step) && e.key !== 'PageUp' && e.key !== 'PageDown') return;
    e.preventDefault();
    const cur = parseIso(this.focusIso || this.min);
    if (e.key === 'PageUp' || e.key === 'PageDown') cur.setMonth(cur.getMonth() + (e.key === 'PageDown' ? 1 : -1));
    else cur.setDate(cur.getDate() + step[e.key]);
    let iso = isoOf(cur);
    if (iso < this.min) iso = this.min;
    if (iso > this.max) iso = this.max;
    const t = parseIso(iso);
    if (t.getFullYear() !== this.month.getFullYear() || t.getMonth() !== this.month.getMonth()) {
      this.month = new Date(t.getFullYear(), t.getMonth(), 1);
    }
    this.render(iso);
  }
}

/** Срок: пилюли и свой выбор. onChange — после любой смены. */
export class DuePicker {
  private date: HTMLInputElement;
  private time: HTMLInputElement;
  private custom: HTMLButtonElement;
  private panel: HTMLElement;
  private presets: HTMLButtonElement[];
  private cal: Calendar | null = null;
  private sum: HTMLElement;

  constructor(root: HTMLElement, private onChange: () => void) {
    this.date = $<HTMLInputElement>('[name="publishBy"]', root)!;
    this.time = $<HTMLInputElement>('[name="publishTime"]', root)!;
    this.custom = $<HTMLButtonElement>('[data-due-custom]', root)!;
    this.panel = $('[data-due-panel]', root)!;
    this.sum = $('[data-due-sum]', root)!;
    this.presets = $$<HTMLButtonElement>('[data-days]', root);

    // открыли сохранённое: совпадает с пилюлей (без времени) — она, иначе «Свой»
    const iso = fromDotted(this.date.value);
    const preset = !this.time.value && this.presets.find((b) => isoDay(Number(b.dataset.days)) === iso);
    if (preset) this.mark(preset);
    else if (this.date.value || this.time.value) this.openCustom(false);

    this.presets.forEach((b) =>
      b.addEventListener('click', () => {
        this.date.value = dotted(isoDay(Number(b.dataset.days)));
        this.time.value = '';
        this.mark(b);
        this.closeCustom();
        this.onChange();
      }),
    );
    this.custom.addEventListener('click', () => {
      if (!this.panel.hidden && !this.date.value) return this.closeCustom(true);
      this.openCustom(true);
    });
    // время (по желанию): ЧЧ:ММ, двоеточие ставится само
    this.time.addEventListener('input', () => {
      const d = this.time.value.replace(/\D/g, '').slice(0, 4);
      this.time.value = d.length > 2 ? `${d.slice(0, 2)}:${d.slice(2)}` : d;
      this.summary();
    });
  }

  /** ГГГГ-ММ-ДД или ''. */
  iso(): string {
    return fromDotted(this.date.value);
  }

  /** Поле, к которому вести при ошибке. */
  focusTarget(): HTMLElement {
    if (!this.panel.hidden) return $<HTMLElement>('.cal-d[tabindex="0"]', this.panel) ?? this.custom;
    return this.presets.find((b) => b.getAttribute('aria-pressed') === 'true') ?? this.presets[0];
  }

  private mark(on: HTMLButtonElement | null) {
    [...this.presets, this.custom].forEach((b) => b.setAttribute('aria-pressed', String(b === on)));
  }

  private openCustom(focus: boolean) {
    this.mark(this.custom);
    this.custom.setAttribute('aria-expanded', 'true');
    const wasHidden = this.panel.hidden;
    this.panel.hidden = false;
    if (!this.cal) {
      this.cal = new Calendar($('[data-cal]', this.panel)!, isoDay(1), isoDay(90), this.iso(), (iso) => {
        this.date.value = dotted(iso);
        this.summary();
        this.onChange();
      });
    } else this.cal.set(this.iso());
    if (wasHidden) appear(this.panel);
    this.summary();
    if (focus) $<HTMLButtonElement>('.cal-d[tabindex="0"]', this.panel)?.focus();
  }

  private closeCustom(refocus = false) {
    this.panel.hidden = true;
    this.custom.setAttribute('aria-expanded', 'false');
    if (refocus) {
      this.custom.setAttribute('aria-pressed', 'false');
      this.custom.focus();
    }
  }

  private summary() {
    const iso = this.iso();
    if (!iso) return void (this.sum.textContent = 'Выберите дату');
    const d = parseIso(iso);
    const t = /^([01]\d|2[0-3]):[0-5]\d$/.test(this.time.value) ? `, ${this.time.value} по Москве` : ', до конца дня';
    this.sum.textContent = `До ${d.getDate()} ${MONTHS_GEN[d.getMonth()]}${t}`;
  }
}
