// Конструктор блоков страницы бизнеса и профиля автора: добавить (список видов с подсказками), удалить (с возвратом
// из сообщения), перетащить за ручку или Alt+↑/↓. У каждого вида — свои крупные поля: текст, пункты, фото (загрузчик),
// видео (файл или ссылка), контакты, темы. «Свои» блоки автора (каналы, отзывы, анкета) наполняются сами. Лимиты —
// как у сервера (service/pages_check.go).
import type { components } from '../../lib/api.gen';
import { PAGE_ABOUT_CREATOR, PAGE_BLOCKS, PAGE_SINGLE, PAGE_TYPES, type PageBlockType, type PageKind } from '../../content/app';
import { toast } from '../../lib/ui';
import { CategoryPicker } from './category';
import { appear, autosize, h, ic, vanish, type IconName } from './dom';
import { sortable } from './sortable';
import { Uploader, type Photo } from './uploader';
import { VideoList, type EdVideo } from './video';

type Item = components['schemas']['CategoryItem'];
type Contacts = components['schemas']['PageContacts'];

/** Блок в редакторе (и в черновике формы): фото — с адресами превью, видео — как показано. */
export interface EdBlock {
  type: PageBlockType;
  title: string;
  text: string;
  items: string[];
  photos: Photo[];
  videos: EdVideo[];
  links: string[];
  contacts: Contacts | null;
}

export const MAX_PAGE_BLOCKS = 20;
const TEXT_MAX: Partial<Record<PageBlockType, number>> = { about: 3000, text: 3000, works: 1000 };
const ICON: Record<PageBlockType, IconName> = {
  about: 'text', photos: 'imagePlus', video: 'video', contacts: 'mapPin', facts: 'listChecks', text: 'text',
  channels: 'badge', topics: 'tag', works: 'camera', reviews: 'heart', card: 'idCard',
};
const AUTO_NOTE: Partial<Record<PageBlockType, string>> = {
  channels: 'Подтверждённые соцсети из анкеты появятся здесь сами — с цифрами от площадки.',
  reviews: 'Отзывы появятся после первых сделок. Пока посетители увидят «Отзывов пока нет».',
  card: 'Опубликованная анкета (услуги, форматы и цены) появится здесь сама.',
};

export const emptyBlock = (type: PageBlockType): EdBlock => ({
  type, title: '', text: '', items: [], photos: [], videos: [], links: [], contacts: type === 'contacts' ? { city: '', address: '', hours: '', site: '', phone: '', socials: [] } : null,
});

interface Part {
  value(): Partial<EdBlock>;
  busy?(): boolean;
  titles?(): string[];
}

export interface PageBlocksDeps {
  kind: PageKind;
  categories: { common: Item[]; own: Item[] };
  onChange: () => void;
  onBusy: (b: boolean) => void;
}

const lines = (s: string) => s.split('\n').map((x) => x.trim()).filter(Boolean);

export class PageBlocks {
  private list: HTMLElement;
  private menu: HTMLElement;
  private parts = new WeakMap<HTMLElement, Part>();

  constructor(root: HTMLElement, blocks: EdBlock[], private d: PageBlocksDeps) {
    this.list = h('div', { class: 'pbs' });
    this.menu = h('div', { class: 'pb-menu', role: 'group', 'aria-label': 'Добавить блок' });
    root.append(this.list, h('p', { class: 'lbl pb-add-t' }, 'Добавить блок'), this.menu);
    blocks.forEach((b) => this.list.append(this.block(b)));
    sortable(this.list, {
      item: '.pb', handle: '.pb-grip', onChange: () => this.changed(),
      label: (el) => `Блок «${this.name(el.dataset.type as PageBlockType)}»`,
    });
    this.sync();
  }

  /** Блоки по порядку (номер — как у ошибок сервера blocks.N). */
  value(): EdBlock[] {
    return this.els().map((el) => ({ ...emptyBlock(el.dataset.type as PageBlockType), title: titleOf(el), ...this.parts.get(el)!.value() }));
  }

  /** Названия выбранных тем (для «Как увидят»). */
  topicTitles(): string[] {
    const el = this.els().find((x) => x.dataset.type === 'topics');
    return el ? this.parts.get(el)!.titles?.() ?? [] : [];
  }

  busy(): boolean {
    return this.els().some((el) => this.parts.get(el)!.busy?.());
  }

  showErrors(f: Record<string, string>) {
    this.els().forEach((el, i) => {
      el.querySelector('.pb-err')!.textContent = f[`blocks.${i}`] ?? '';
      el.classList.toggle('is-bad', !!f[`blocks.${i}`]);
    });
  }

  /** Куда вести «Осталось: блоки» — первое поле блока или список «Добавить». */
  first(): HTMLElement | null {
    return this.list.querySelector<HTMLElement>('.pb textarea, .pb .inp, .pb .up-add') ?? this.menu.querySelector('button');
  }

  private els() {
    return [...this.list.querySelectorAll<HTMLElement>(':scope > .pb')];
  }

  private name(t: PageBlockType) {
    return t === 'about' && this.d.kind === 'creator' ? PAGE_ABOUT_CREATOR : PAGE_BLOCKS[t].title;
  }

  private changed() {
    this.sync();
    this.d.onChange();
  }

  /** «Добавить блок»: виды этой страницы; одиночные — пока их нет; не больше 20. */
  private sync() {
    const have = new Set(this.els().map((el) => el.dataset.type));
    this.menu.replaceChildren();
    if (this.els().length >= MAX_PAGE_BLOCKS) {
      this.menu.append(h('p', { class: 'muted' }, `Блоков — не больше ${MAX_PAGE_BLOCKS}`));
      return;
    }
    for (const t of PAGE_TYPES[this.d.kind]) {
      if (PAGE_SINGLE.includes(t) && have.has(t)) continue;
      const b = h('button', { class: 'pb-new', type: 'button', html: ic(ICON[t], 22) }, h('span', {}, h('b', {}, this.name(t)), h('small', {}, PAGE_BLOCKS[t].hint)));
      b.addEventListener('click', () => {
        const el = this.block(emptyBlock(t));
        this.list.append(el);
        appear(el);
        el.scrollIntoView({ block: 'nearest', behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
        el.querySelector<HTMLElement>('textarea, .inp, .up-add, .chip')?.focus({ preventScroll: true });
        this.changed();
      });
      this.menu.append(b);
    }
  }

  private block(b: EdBlock): HTMLElement {
    const el = h('section', { class: 'pb', 'data-type': b.type, 'aria-label': this.name(b.type) });
    const grip = h('button', { class: 'pb-grip grip', type: 'button', 'aria-label': 'Переместить блок: стрелки или Alt+↑/↓', html: ic('grip', 20) });
    const del = h('button', { class: 'ibtn pb-del', type: 'button', 'aria-label': `Удалить блок «${this.name(b.type)}»`, html: ic('trash', 18) });
    const title = h('input', {
      class: 'pb-title', type: 'text', maxlength: '80', 'aria-label': 'Заголовок блока',
      placeholder: b.type === 'text' ? 'Заголовок' : this.name(b.type),
    });
    title.value = b.title;
    const body = h('div', { class: 'pb-body' });
    const err = h('p', { class: 'ferr pb-err', 'aria-live': 'polite' });
    el.append(h('div', { class: 'pb-h' }, grip, h('span', { class: 'pb-ic', html: ic(ICON[b.type], 20) }), title, del), body, err);
    this.parts.set(el, this.fill(b, body));

    title.addEventListener('input', () => this.d.onChange());
    el.addEventListener('keydown', (e) => {
      if (!e.altKey || (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') || !(e.target as HTMLElement).matches('.pb-title, .pb-grip')) return;
      e.preventDefault();
      const sib = e.key === 'ArrowUp' ? el.previousElementSibling : el.nextElementSibling;
      if (!sib) return;
      sib.insertAdjacentElement(e.key === 'ArrowUp' ? 'beforebegin' : 'afterend', el);
      (e.target as HTMLElement).focus();
      this.changed();
    });
    del.addEventListener('click', () => {
      const next = el.nextElementSibling;
      void vanish(el).then(() => {
        this.changed();
        toast('Блок удалён', () => {
          this.list.insertBefore(el, next && next.parentElement === this.list ? next : null);
          appear(el);
          this.changed();
        });
      });
    });
    return el;
  }

  /** Поля блока по виду; возвращает, как прочитать значение. */
  private fill(b: EdBlock, body: HTMLElement): Part {
    const on = () => this.d.onChange();
    const area = (value: string, max: number, ph: string, label: string, rows = 4) => {
      const t = h('textarea', { class: 'inp pb-area', maxlength: String(max), rows: String(rows), placeholder: ph, 'aria-label': label });
      t.value = value;
      t.addEventListener('input', () => {
        autosize(t);
        on();
      });
      requestAnimationFrame(() => autosize(t));
      body.append(t);
      return t;
    };
    const field = (label: string, value: string, attrs: Record<string, string>) => {
      const id = `pb-${Math.random().toString(36).slice(2, 9)}`;
      const i = h('input', { class: 'inp', id, ...attrs });
      i.value = value;
      i.addEventListener('input', on);
      body.append(h('div', { class: 'field' }, h('label', { class: 'lbl', for: id }, label), i));
      return i;
    };
    const photos = (initial: Photo[]) => {
      const box = h('div', { class: 'pb-photos' });
      body.append(box);
      return new Uploader(box, initial, { purpose: 'page_photo', max: 12, label: 'Добавить фото', onChange: on, onBusy: this.d.onBusy });
    };
    switch (b.type) {
      case 'about':
      case 'text': {
        const t = area(b.text, TEXT_MAX[b.type]!, b.type === 'about' ? (this.d.kind === 'creator' ? 'Кто вы, о чём снимаете, чем полезны бизнесу' : 'Чем вы занимаетесь, для кого и почему к вам идут') : 'Текст — абзацы через пустую строку', 'Текст блока', 5);
        return { value: () => ({ text: t.value.trim() }) };
      }
      case 'facts': {
        const t = area(b.items.join('\n'), 2600, 'По одному на строку: «10 лет на рынке», «3 кофейни в центре»', 'Факты, по одному на строку');
        body.append(h('p', { class: 'muted pb-note' }, 'До 12 пунктов, каждый — с новой строки'));
        return { value: () => ({ items: lines(t.value) }) };
      }
      case 'photos': {
        const up = photos(b.photos);
        return { value: () => ({ photos: up.photos() }), busy: () => up.busy() };
      }
      case 'video': {
        const box = h('div', { class: 'pb-video' });
        body.append(box, h('p', { class: 'muted pb-note' }, 'MP4 или WebM — как есть; или ссылка на ролик YouTube / VK Видео'));
        const v = new VideoList(box, b.videos, 1, on, this.d.onBusy);
        return { value: () => ({ videos: v.value() }), busy: () => v.busy() };
      }
      case 'contacts': {
        const c = b.contacts ?? emptyBlock('contacts').contacts!;
        const grid = h('div', { class: 'row2' });
        const city = field('Город', c.city, { maxlength: '80', autocomplete: 'address-level2', placeholder: 'Казань' });
        const addr = field('Адрес', c.address, { maxlength: '200', autocomplete: 'street-address', placeholder: 'ул. Баумана, 1' });
        const hours = field('Часы работы', c.hours, { maxlength: '120', placeholder: 'Ежедневно 8:00–22:00' });
        const phone = field('Телефон', c.phone, { maxlength: '30', type: 'tel', inputmode: 'tel', autocomplete: 'tel', placeholder: '+7 900 000-00-00' });
        const site = field('Сайт', c.site, { maxlength: '300', type: 'url', inputmode: 'url', placeholder: 'zerno.ru' });
        [city, addr, hours, phone].forEach((i) => grid.append(i.parentElement!));
        body.prepend(grid);
        body.append(h('p', { class: 'lbl pb-sub' }, 'Соцсети'));
        const soc = area(c.socials.join('\n'), 2000, 'По одной ссылке на строку: vk.com/…, t.me/…', 'Соцсети, по одной ссылке на строку', 2);
        return {
          value: () => ({ contacts: { city: city.value.trim(), address: addr.value.trim(), hours: hours.value.trim(), phone: phone.value.trim(), site: site.value.trim(), socials: lines(soc.value).slice(0, 6) } }),
        };
      }
      case 'topics': {
        const box = h('div', { class: 'pb-topics' });
        body.append(box);
        const known = new Set([...this.d.categories.common, ...this.d.categories.own].map((c) => c.slug));
        const p = new CategoryPicker(box, this.d.categories.common, this.d.categories.own, b.items.filter((s) => known.has(s)), {
          multi: true, max: 10, maxCustom: 3, label: 'Что могу рекламировать', addLabel: 'Своя тема', onChange: on,
        });
        return { value: () => ({ items: p.value() }), titles: () => p.titles() };
      }
      case 'works': {
        const t = area(b.text, 1000, 'Коротко: что снимали и для кого (по желанию)', 'Описание работ', 2);
        const up = photos(b.photos);
        body.append(h('p', { class: 'lbl pb-sub' }, 'Видео'));
        const box = h('div', { class: 'pb-video' });
        body.append(box);
        const v = new VideoList(box, b.videos, 3, on, this.d.onBusy);
        body.append(h('p', { class: 'lbl pb-sub' }, 'Ссылки на публикации'));
        const links = area(b.links.join('\n'), 3000, 'По одной на строку: t.me/канал/123, vk.com/clip-…', 'Ссылки, по одной на строку', 2);
        return {
          value: () => ({ text: t.value.trim(), photos: up.photos(), videos: v.value(), links: lines(links.value).slice(0, 10) }),
          busy: () => up.busy() || v.busy(),
        };
      }
      default:
        body.append(h('p', { class: 'muted pb-note' }, AUTO_NOTE[b.type] ?? ''));
        return { value: () => ({}) };
    }
  }
}

const titleOf = (el: HTMLElement) => el.querySelector<HTMLInputElement>('.pb-title')!.value.trim();
