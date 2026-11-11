// Просмотр фото поверх страницы (как в Telegram): все фото группы ([data-lb-group], иначе страница) по порядку,
// счётчик «3 / 9», стрелки и миниатюры, листание клавишами ←/→ (Home/End) и свайпом; Esc,
// ✕ или щелчок по фону — закрыть, фокус возвращается на открытое фото. Свой диалог (<dialog> — фокус внутри,
// страница недоступна), плавные переходы; при «уменьшить движение» — без анимаций.
import { h, ic } from './editor/dom';

interface Shot { src: string; large: string; thumb: string; trigger: HTMLElement }

const reduce = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
const EASE = 'cubic-bezier(.2,.7,.2,1)';

class Lightbox {
  private dlg = h('dialog', { class: 'lb', 'aria-label': 'Просмотр фото' });
  private count = h('p', { class: 'lb-count', 'aria-live': 'polite' });
  private img = h('img', { class: 'lb-img', alt: '', decoding: 'async', draggable: 'false' });
  private stage = h('div', { class: 'lb-stage' });
  private prev = h('button', { class: 'lb-nav lb-prev', type: 'button', 'aria-label': 'Предыдущее фото', html: ic('left', 24) });
  private next = h('button', { class: 'lb-nav lb-next', type: 'button', 'aria-label': 'Следующее фото', html: ic('right', 24) });
  private close = h('button', { class: 'lb-x', type: 'button', 'aria-label': 'Закрыть', html: ic('x', 24) });
  private strip = h('div', { class: 'lb-strip', role: 'group', 'aria-label': 'Все фото' });
  private shots: Shot[] = [];
  private i = 0;
  private opener: HTMLElement | null = null; // фокус после закрытия — на открывшее фото

  constructor() {
    const fig = h('figure', { class: 'lb-fig' }, this.img);
    this.stage.append(this.prev, fig, this.next);
    this.dlg.append(h('div', { class: 'lb-top' }, this.count, this.close), this.stage, this.strip);
    document.body.append(this.dlg);

    this.prev.addEventListener('click', () => this.go(this.i - 1, -1));
    this.next.addEventListener('click', () => this.go(this.i + 1, 1));
    this.close.addEventListener('click', () => this.hide());
    this.dlg.addEventListener('cancel', (e) => {
      e.preventDefault(); // своё закрытие — с анимацией и возвратом фокуса
      this.hide();
    });
    this.dlg.addEventListener('close', () => this.cleanup());
    // щелчок по фону (не по фото, стрелкам и миниатюрам) — закрыть
    this.dlg.addEventListener('click', (e) => {
      const t = e.target as HTMLElement;
      if (t === this.dlg || t === this.stage || t.classList.contains('lb-fig') || t.classList.contains('lb-top')) this.hide();
    });
    this.dlg.addEventListener('keydown', (e) => {
      if (e.key === 'ArrowLeft') this.go(this.i - 1, -1);
      else if (e.key === 'ArrowRight') this.go(this.i + 1, 1);
      else if (e.key === 'Home') this.go(0, -1);
      else if (e.key === 'End') this.go(this.shots.length - 1, 1);
      else return;
      e.preventDefault();
    });
    this.swipe();
  }

  open(shots: Shot[], i: number) {
    this.shots = shots;
    this.opener = shots[i]?.trigger ?? null;
    this.strip.replaceChildren(
      ...shots.map((s, j) => {
        const b = h('button', { class: 'lb-th', type: 'button', 'aria-label': `Фото ${j + 1} из ${shots.length}` }, h('img', { src: s.thumb, alt: '', loading: 'lazy', decoding: 'async' }));
        b.addEventListener('click', () => this.go(j, j < this.i ? -1 : 1));
        return b;
      }),
    );
    const many = shots.length > 1;
    this.prev.hidden = this.next.hidden = this.strip.hidden = !many;
    document.documentElement.classList.add('lb-lock');
    this.dlg.showModal();
    this.show(i, 0);
    (many ? this.next : this.close).focus();
    if (!reduce()) this.dlg.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 200, easing: EASE });
  }

  private hide() {
    if (!this.dlg.open) return;
    if (reduce()) return this.dlg.close();
    this.dlg.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 160, easing: 'ease' }).finished.finally(() => this.dlg.close());
  }

  private cleanup() {
    document.documentElement.classList.remove('lb-lock');
    this.opener?.focus({ preventScroll: true });
  }

  /** Перейти к фото j (по кругу нет: на краях стрелка недоступна). dir — откуда въезжает новое. */
  private go(j: number, dir: number) {
    if (j < 0 || j >= this.shots.length || j === this.i) return;
    this.show(j, dir);
  }

  private show(j: number, dir: number) {
    this.i = j;
    const s = this.shots[j];
    this.img.src = s.src;
    this.img.srcset = `${s.src} 800w, ${s.large} 1600w`;
    this.img.sizes = '100vw';
    this.count.textContent = `${j + 1} / ${this.shots.length}`;
    this.prev.disabled = j === 0;
    this.next.disabled = j === this.shots.length - 1;
    [...this.strip.children].forEach((b, k) => (k === j ? b.setAttribute('aria-current', 'true') : b.removeAttribute('aria-current')));
    this.strip.children[j]?.scrollIntoView({ block: 'nearest', inline: 'center', behavior: reduce() ? 'auto' : 'smooth' });
    if (dir && !reduce()) {
      this.img.animate([{ opacity: 0, transform: `translateX(${dir * 40}px)` }, { opacity: 1, transform: 'none' }], { duration: 220, easing: EASE });
    }
    // соседние — заранее, чтобы листание было мгновенным
    for (const k of [j - 1, j + 1]) {
      const n = this.shots[k];
      if (n) new Image().src = n.src;
    }
  }

  /** Свайп по фото: тянется за пальцем, дальше 60px (или быстро) — следующее/предыдущее. */
  private swipe() {
    let x0 = 0;
    let t0 = 0;
    let id = -1;
    this.stage.addEventListener('pointerdown', (e) => {
      if (e.pointerType === 'mouse' || this.shots.length < 2 || (e.target as HTMLElement).closest('button')) return;
      id = e.pointerId;
      x0 = e.clientX;
      t0 = performance.now();
    });
    this.stage.addEventListener('pointermove', (e) => {
      if (e.pointerId !== id) return;
      this.img.style.transform = `translateX(${e.clientX - x0}px)`;
    });
    const end = (e: PointerEvent) => {
      if (e.pointerId !== id) return;
      id = -1;
      const dx = e.clientX - x0;
      this.img.style.transform = '';
      const fast = Math.abs(dx) > 20 && performance.now() - t0 < 250;
      if (Math.abs(dx) > 60 || fast) this.go(this.i + (dx < 0 ? 1 : -1), dx < 0 ? 1 : -1);
    };
    this.stage.addEventListener('pointerup', end);
    this.stage.addEventListener('pointercancel', end);
  }
}

let box: Lightbox | null = null;

document.addEventListener('click', (e) => {
  const t = (e.target as HTMLElement).closest<HTMLElement>('[data-lb]');
  if (!t) return;
  e.preventDefault();
  const group = t.closest('[data-lb-group]') ?? document;
  const triggers = [...group.querySelectorAll<HTMLElement>('[data-lb]')];
  const shots = triggers.map((el) => ({ src: el.dataset.src!, large: el.dataset.large!, thumb: el.dataset.thumb!, trigger: el }));
  box ??= new Lightbox();
  box.open(shots, triggers.indexOf(t));
});
