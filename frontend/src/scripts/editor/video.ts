// Видео в блоке страницы: файл (MP4 / WebM — как есть, без перекодирования) или ссылка на ролик YouTube / VK Видео.
// Файл грузится с прогрессом и отменой; кадр-заставку снимаем в браузере (видео → canvas → JPEG) и грузим как фото
// video_poster — не вышло (кодек, браузер) — без заставки. Ссылку проверяем по белому списку сразу (окончательно — сервер,
// он же строит адрес для встраивания).
import type { components } from '../../lib/api.gen';
import { csrfToken } from '../../lib/client';
import { toast } from '../../lib/ui';
import { h, ic, vanish } from './dom';

type Photo = components['schemas']['Photo'];

/** Видео в редакторе: файл (media, src, poster) или ссылка (url). */
export interface EdVideo {
  media?: string;
  src?: string;
  poster?: Photo | null;
  url?: string;
}

const LINK = /^(https?:\/\/)?((www|m)\.)?(youtube\.com\/(watch\?.*v=|shorts\/|embed\/|live\/)[A-Za-z0-9_-]{11}|youtu\.be\/[A-Za-z0-9_-]{11}|(vk\.com|vk\.ru|vkvideo\.ru)\/.*(video|clip)-?\d+_\d+)/i;
export const isVideoLink = (s: string) => LINK.test(s.trim());
const isVideoFile = (f: File) => /^video\/(mp4|webm)$/.test(f.type) || /\.(mp4|m4v|webm)$/i.test(f.name);

/** Список видео блока (до max): каждое — файл или ссылка. */
export class VideoList {
  private list: HTMLElement;
  private add: HTMLElement;
  private pending = 0;

  constructor(root: HTMLElement, initial: EdVideo[], private max: number, private onChange: () => void, private onBusy: (b: boolean) => void) {
    this.list = h('div', { class: 'vd-list' });
    const file = h('button', { class: 'chip sm', type: 'button', html: ic('video', 16) }, 'Загрузить видео');
    const link = h('button', { class: 'chip sm', type: 'button', html: ic('link', 16) }, 'Ссылка YouTube / VK');
    this.add = h('div', { class: 'vd-add', role: 'group', 'aria-label': 'Добавить видео' }, file, link);
    const input = h('input', { type: 'file', accept: 'video/mp4,video/webm,.mp4,.m4v,.webm', class: 'sr-only', tabindex: '-1', 'aria-hidden': 'true' });
    root.append(this.list, this.add, input);
    initial.forEach((v) => this.list.append(v.url ? this.linkRow(v.url) : this.fileRow(v)));
    file.addEventListener('click', () => input.click());
    input.addEventListener('change', () => {
      const f = input.files?.[0];
      input.value = '';
      if (f) this.upload(f);
    });
    link.addEventListener('click', () => {
      const row = this.linkRow('');
      this.list.append(row);
      this.sync();
      row.querySelector('input')!.focus();
    });
    this.sync();
  }

  /** Видео по порядку (пустые ссылки и незагруженные файлы — нет). */
  value(): EdVideo[] {
    return [...this.list.querySelectorAll<HTMLElement>('.vd')].flatMap((r): EdVideo[] => {
      if (r.dataset.kind === 'link') {
        const u = r.querySelector<HTMLInputElement>('input')!.value.trim();
        return u ? [{ url: u }] : [];
      }
      return r.dataset.media ? [{ media: r.dataset.media, src: r.dataset.src, poster: r.dataset.poster ? (JSON.parse(r.dataset.poster) as Photo) : null }] : [];
    });
  }

  busy(): boolean {
    return this.pending > 0;
  }

  private sync() {
    this.add.hidden = this.list.querySelectorAll('.vd').length >= this.max;
  }

  private removeBtn(row: HTMLElement, onRemove?: () => void) {
    const rm = h('button', { class: 'ibtn vd-rm', type: 'button', 'aria-label': 'Убрать видео', html: ic('x', 18) });
    rm.addEventListener('click', () => {
      onRemove?.();
      void vanish(row).then(() => {
        this.sync();
        this.onChange();
      });
    });
    return rm;
  }

  private linkRow(url: string): HTMLElement {
    const row = h('div', { class: 'vd', 'data-kind': 'link' });
    const inp = h('input', { class: 'inp', type: 'url', inputmode: 'url', maxlength: '500', placeholder: 'https://youtu.be/… или https://vk.com/video-…', 'aria-label': 'Ссылка на видео' });
    inp.value = url;
    const err = h('p', { class: 'ferr', 'aria-live': 'polite' });
    const check = () => {
      const v = inp.value.trim();
      const bad = !!v && !isVideoLink(v);
      err.textContent = bad ? 'Подойдёт ссылка на ролик YouTube или VK Видео' : '';
      inp.setAttribute('aria-invalid', String(bad));
    };
    inp.addEventListener('input', () => {
      if (inp.getAttribute('aria-invalid') === 'true') check();
      this.onChange();
    });
    inp.addEventListener('blur', check);
    row.append(h('span', { class: 'vd-ic', html: ic('link', 20) }), h('div', { class: 'vd-body' }, inp, err), this.removeBtn(row));
    return row;
  }

  private fileRow(v: EdVideo): HTMLElement {
    const row = h('div', { class: 'vd', 'data-kind': 'file', 'data-media': v.media ?? '', 'data-src': v.src ?? '' });
    if (v.poster) row.dataset.poster = JSON.stringify(v.poster);
    const pv = h('video', { class: 'vd-pv', src: v.src ?? '', preload: 'metadata', muted: true, playsinline: true, controls: true });
    if (v.poster) pv.poster = v.poster.src;
    row.append(pv, this.removeBtn(row));
    return row;
  }

  private upload(f: File) {
    if (!isVideoFile(f)) return toast('Подойдёт видео MP4 или WebM');
    const row = h('div', { class: 'vd is-loading', 'data-kind': 'file' });
    const bar = h('span', { class: 'up-bar' }, h('i'));
    const label = h('p', { class: 'vd-name' }, f.name);
    const state = h('p', { class: 'muted', 'aria-live': 'polite' }, 'Загружается…');
    const xhr = new XMLHttpRequest();
    row.append(h('span', { class: 'vd-ic', html: ic('video', 20) }), h('div', { class: 'vd-body' }, label, state, bar), this.removeBtn(row, () => xhr.abort()));
    this.list.append(row);
    this.sync();
    this.pending++;
    this.onBusy(true);
    const done = () => {
      this.pending--;
      if (!this.pending) this.onBusy(false);
    };
    const fail = (msg: string) => {
      done();
      row.classList.remove('is-loading');
      row.classList.add('is-error');
      bar.remove();
      state.textContent = msg;
      state.className = 'ferr';
    };
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) {
        const pct = Math.round((e.loaded / e.total) * 100);
        (bar.firstChild as HTMLElement).style.width = `${pct}%`;
        state.textContent = pct < 100 ? `Загружается… ${pct}%` : 'Сохраняем…';
      }
    };
    xhr.onabort = () => done();
    xhr.onerror = () => fail('Нет связи — попробуйте ещё раз');
    xhr.ontimeout = () => fail('Слишком долго — попробуйте файл поменьше');
    xhr.onload = async () => {
      let body: { id?: string; src?: string; error?: { message: string } } = {};
      try {
        body = JSON.parse(xhr.responseText);
      } catch {
        /* не JSON — общая ошибка ниже */
      }
      if (xhr.status === 401) return location.assign('/login');
      if (xhr.status !== 201 || !body.id) return fail(body.error?.message ?? 'Не загрузилось');
      state.textContent = 'Готовим заставку…';
      const poster = await posterOf(f).catch(() => null);
      done();
      const ready = this.fileRow({ media: body.id, src: body.src, poster });
      row.replaceWith(ready);
      this.sync();
      this.onChange();
    };
    void csrfToken().then((tok) => {
      const form = new FormData();
      form.append('file', f);
      xhr.open('POST', '/api/me/media/video');
      xhr.timeout = 600_000;
      xhr.setRequestHeader('X-CSRF-Token', tok);
      xhr.send(form);
    }, () => fail('Нет связи'));
  }
}

/** Кадр-заставка: первая секунда (или треть короткого ролика) → JPEG до 1280 px → фото video_poster. */
async function posterOf(f: File): Promise<Photo | null> {
  const url = URL.createObjectURL(f);
  try {
    const v = document.createElement('video');
    v.muted = true;
    v.playsInline = true;
    v.preload = 'auto';
    v.src = url;
    await new Promise<void>((ok, bad) => {
      v.onloadedmetadata = () => ok();
      v.onerror = () => bad(new Error('видео не открылось'));
      setTimeout(() => bad(new Error('долго')), 8000);
    });
    v.currentTime = Math.min(1, (v.duration || 0) / 3);
    await new Promise<void>((ok, bad) => {
      v.onseeked = () => ok();
      setTimeout(() => bad(new Error('долго')), 8000);
    });
    const scale = Math.min(1, 1280 / Math.max(v.videoWidth, v.videoHeight, 1));
    const c = document.createElement('canvas');
    c.width = Math.round(v.videoWidth * scale);
    c.height = Math.round(v.videoHeight * scale);
    if (!c.width || !c.height) return null;
    c.getContext('2d')!.drawImage(v, 0, 0, c.width, c.height);
    const blob = await new Promise<Blob | null>((ok) => c.toBlob(ok, 'image/jpeg', 0.85));
    if (!blob) return null;
    const form = new FormData();
    form.append('purpose', 'video_poster');
    form.append('file', blob, 'poster.jpg');
    const res = await fetch('/api/me/media', { method: 'POST', body: form, headers: { 'X-CSRF-Token': await csrfToken() } });
    if (res.status !== 201) return null;
    const b = (await res.json()) as Photo;
    return { id: b.id, thumb: b.thumb, src: b.src, large: b.large };
  } finally {
    URL.revokeObjectURL(url);
  }
}
