// Загрузка фото: выбор файла (на телефоне — камера или галерея) и перетаскивание, превью сразу, прогресс, отмена,
// удаление, перестановка, обрезка аватара и обложки. Тип и размер проверяются до отправки (понятная ошибка у фото),
// окончательно — на сервере (по содержимому). Фото попадает в задание или анкету их сохранением.
import type { components } from '../../lib/api.gen';
import { csrfToken } from '../../lib/client';
import { toast } from '../../lib/ui';
import { cropDialog, type Crop } from './crop';
import { appear, h, ic, vanish } from './dom';
import { sortable } from './sortable';

export type Photo = components['schemas']['Photo'];
type Purpose = components['schemas']['MediaPurpose'];

const MAX_BYTES = 10 << 20;
const TYPES = ['image/jpeg', 'image/png', 'image/webp', 'image/heic', 'image/heif'];
const isImage = (f: File) => TYPES.includes(f.type) || /\.(jpe?g|png|webp|hei[cf])$/i.test(f.name);

export interface UploaderOptions {
  purpose: Purpose;
  max: number;
  aspect?: number; // обрезка перед загрузкой
  round?: boolean; // круглое превью (аватар)
  label: string; // подпись кнопки добавления
  onChange: () => void;
  onBusy?: (busy: boolean) => void;
}

export class Uploader {
  private grid: HTMLElement;
  private add: HTMLButtonElement;
  private input: HTMLInputElement;
  private pending = 0;

  constructor(root: HTMLElement, initial: Photo[], private o: UploaderOptions) {
    root.classList.add('up', o.max === 1 ? 'is-single' : 'is-grid');
    if (o.round) root.classList.add('is-round');
    if (o.aspect && o.aspect > 1) root.classList.add('is-wide');
    this.grid = h('div', { class: 'up-grid' });
    this.input = h('input', { type: 'file', accept: 'image/*,.heic,.heif', class: 'sr-only', tabindex: '-1', 'aria-hidden': 'true', multiple: o.max > 1 });
    this.add = h('button', { class: 'up-add', type: 'button', html: ic('imagePlus', 24) }, h('span', {}, o.label));
    root.append(this.grid, this.input);
    this.grid.append(this.add);
    initial.forEach((p) => this.grid.insertBefore(this.tile(p), this.add));
    this.sync();

    this.add.addEventListener('click', () => this.input.click());
    this.input.addEventListener('change', () => {
      void this.take([...(this.input.files ?? [])]);
      this.input.value = '';
    });
    // перетаскивание файлов на область
    root.addEventListener('dragover', (e) => {
      if (!e.dataTransfer?.types.includes('Files')) return;
      e.preventDefault();
      root.classList.add('is-over');
    });
    root.addEventListener('dragleave', (e) => {
      if (!root.contains(e.relatedTarget as Node)) root.classList.remove('is-over');
    });
    root.addEventListener('drop', (e) => {
      if (!e.dataTransfer?.files.length) return;
      e.preventDefault();
      root.classList.remove('is-over');
      void this.take([...e.dataTransfer.files]);
    });
    if (o.max > 1) {
      sortable(this.grid, { item: '.up-t[data-id]', handle: '.up-grip', grid: true, onChange: () => o.onChange(), label: () => 'Фото' });
    }
  }

  /** id загруженных фото по порядку. */
  value(): string[] {
    return [...this.grid.querySelectorAll<HTMLElement>('.up-t[data-id]')].map((t) => t.dataset.id!);
  }

  /** Превью загруженных (для предпросмотра). */
  photos(): Photo[] {
    return [...this.grid.querySelectorAll<HTMLElement>('.up-t[data-id]')].map((t) => JSON.parse(t.dataset.photo!) as Photo);
  }

  busy(): boolean {
    return this.pending > 0;
  }

  private count() {
    return this.grid.querySelectorAll('.up-t').length;
  }

  private sync() {
    this.add.hidden = this.o.max > 1 ? this.count() >= this.o.max : this.count() > 0;
  }

  private async take(files: File[]) {
    const free = this.o.max - this.value().length - this.grid.querySelectorAll('.up-t.is-loading').length;
    if (this.o.max === 1) {
      files = files.slice(0, 1); // одно фото — заменяется после успешной загрузки
    } else if (files.length > free) {
      toast(`Не больше ${this.o.max} фото`);
      files = files.slice(0, Math.max(0, free));
    }
    for (const f of files) {
      if (!isImage(f)) {
        toast('Подойдёт фото JPEG, PNG, WebP или HEIC');
        continue;
      }
      if (f.size > MAX_BYTES) {
        toast('Фото больше 10 МБ');
        continue;
      }
      let crop: Crop | null = null;
      if (this.o.aspect) {
        const c = await cropDialog(f, this.o.aspect, !!this.o.round);
        if (c === 'cancel') continue;
        crop = c;
      }
      this.upload(f, crop);
    }
    this.sync();
  }

  /** Плитка загруженного фото. */
  private tile(p: Photo): HTMLElement {
    const t = h('div', { class: 'up-t', 'data-id': p.id, 'data-photo': JSON.stringify(p) });
    const img = h('img', { src: this.o.max === 1 && !this.o.round ? p.src : p.thumb, alt: '', loading: 'lazy', decoding: 'async' });
    t.append(img, this.controls(t));
    return t;
  }

  private controls(t: HTMLElement): HTMLElement {
    const box = h('div', { class: 'up-ctl' });
    if (this.o.max > 1) box.append(h('button', { class: 'up-grip grip', type: 'button', 'aria-label': 'Переместить фото (стрелки)', html: ic('grip', 18) }));
    else box.append(h('button', { class: 'up-btn', type: 'button', 'aria-label': 'Заменить фото', html: ic('crop', 18) }));
    const rm = h('button', { class: 'up-btn', type: 'button', 'aria-label': 'Убрать фото', html: ic('x', 18) });
    box.append(rm);
    box.querySelector('[aria-label="Заменить фото"]')?.addEventListener('click', () => this.input.click());
    rm.addEventListener('click', () => {
      void vanish(t).then(() => {
        this.sync();
        this.o.onChange();
        this.add.focus();
      });
    });
    return box;
  }

  private upload(file: File, crop: Crop | null) {
    const t = h('div', { class: 'up-t is-loading' });
    const url = URL.createObjectURL(file);
    const img = h('img', { src: url, alt: '' });
    img.onerror = () => img.replaceWith(h('span', { class: 'up-ph', html: ic('camera', 24) }));
    const bar = h('span', { class: 'up-bar' }, h('i'));
    const cancel = h('button', { class: 'up-btn up-cancel', type: 'button', 'aria-label': 'Отменить загрузку', html: ic('x', 18) });
    const state = h('span', { class: 'sr-only', 'aria-live': 'polite' }, 'Загружается');
    t.append(img, bar, cancel, state);
    this.grid.insertBefore(t, this.add);
    appear(t);
    this.sync();

    this.pending++;
    this.o.onBusy?.(true);
    const finish = () => {
      this.pending--;
      URL.revokeObjectURL(url);
      if (!this.pending) this.o.onBusy?.(false);
    };

    const form = new FormData();
    form.append('purpose', this.o.purpose);
    if (crop) form.append('crop', `${crop.x},${crop.y},${crop.w},${crop.h}`);
    form.append('file', file);
    const xhr = new XMLHttpRequest();
    cancel.addEventListener('click', () => xhr.abort());
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) (bar.firstChild as HTMLElement).style.width = `${Math.round((e.loaded / e.total) * 100)}%`;
      if (e.loaded === e.total) t.classList.add('is-processing');
    };
    const fail = (msg: string) => {
      finish();
      t.classList.remove('is-loading', 'is-processing');
      t.classList.add('is-error');
      bar.remove();
      state.textContent = msg;
      state.className = 'up-err';
      cancel.setAttribute('aria-label', 'Убрать');
      cancel.onclick = () => void vanish(t).then(() => this.sync());
    };
    xhr.onabort = () => {
      finish();
      void vanish(t).then(() => this.sync());
    };
    xhr.onerror = () => fail('Нет связи');
    xhr.ontimeout = () => fail('Слишком долго');
    xhr.onload = () => {
      let body: { id?: string; thumb?: string; src?: string; large?: string; error?: { message: string } } = {};
      try {
        body = JSON.parse(xhr.responseText);
      } catch {
        /* не JSON — общая ошибка ниже */
      }
      if (xhr.status === 401) return location.assign('/login');
      if (xhr.status !== 201 || !body.id) return fail(body.error?.message ?? 'Не загрузилось');
      finish();
      const p: Photo = { id: body.id, thumb: body.thumb!, src: body.src!, large: body.large! };
      const done = this.tile(p);
      if (this.o.max === 1) this.grid.querySelectorAll('.up-t[data-id]').forEach((old) => old.remove());
      t.replaceWith(done);
      this.sync();
      this.o.onChange();
    };
    void csrfToken().then((tok) => {
      xhr.open('POST', '/api/me/media');
      xhr.timeout = 120_000;
      xhr.setRequestHeader('X-CSRF-Token', tok);
      xhr.send(form);
    }, () => fail('Нет связи'));
  }
}
