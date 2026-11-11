// Обрезка перед загрузкой (аватар 1:1, обложка 16:9): рамка по центру, фото двигается пальцем или мышью,
// масштаб — ползунок, колесо, щипок; клавиатура — стрелки и +/−. Результат — область в долях кадра (сервер режет сам,
// исходник не пережимается в браузере). Браузер не умеет показать файл (HEIC в Chrome) — null: сервер обрежет по центру.
import { h } from './dom';

export interface Crop { x: number; y: number; w: number; h: number }

/** Диалог обрезки: Crop — выбранная область, 'cancel' — отмена, null — показать нельзя (резать по центру). */
export async function cropDialog(file: File, aspect: number, round: boolean): Promise<Crop | 'cancel' | null> {
  const url = URL.createObjectURL(file);
  const img = new Image();
  img.decoding = 'async';
  img.src = url;
  try {
    await img.decode();
  } catch {
    URL.revokeObjectURL(url);
    return null;
  }
  const iw = img.naturalWidth;
  const ih = img.naturalHeight;
  if (!iw || !ih) {
    URL.revokeObjectURL(url);
    return null;
  }

  const stage = h('div', { class: `crop-stage ${round ? 'is-round' : ''}`, tabindex: '0', 'aria-label': 'Кадр: двигайте стрелками, масштаб — плюс и минус' });
  img.className = 'crop-img';
  img.alt = '';
  img.draggable = false;
  const frame = h('div', { class: 'crop-frame' });
  stage.append(img, frame);
  const zoom = h('input', { type: 'range', class: 'crop-zoom', min: '1', max: '4', step: '0.01', value: '1', 'aria-label': 'Масштаб' });
  const cancel = h('button', { class: 'btn pill-ghost', type: 'button' }, 'Отмена');
  const ok = h('button', { class: 'btn pill-dark', type: 'button' }, 'Готово');
  const dlg = h('dialog', { class: 'dlg crop', 'aria-label': 'Кадрирование' }, h('div', { class: 'dlg-card' }, h('h2', {}, 'Кадр'), stage, zoom, h('div', { class: 'dlg-act' }, cancel, ok)));
  document.body.append(dlg);
  dlg.showModal();

  // геометрия: рамка — по центру сцены с заданными пропорциями; фото покрывает рамку (scale ≥ cover)
  let sw = 0, shh = 0, fw = 0, fh = 0, base = 1, k = 1, tx = 0, ty = 0;
  const layout = () => {
    sw = stage.clientWidth;
    shh = stage.clientHeight;
    const pad = 16;
    fw = Math.min(sw - pad * 2, (shh - pad * 2) * aspect);
    fh = fw / aspect;
    frame.style.width = `${fw}px`;
    frame.style.height = `${fh}px`;
    base = Math.max(fw / iw, fh / ih);
    clamp();
    paint();
  };
  const clamp = () => {
    const w = iw * base * k, hh = ih * base * k;
    const mx = Math.max(0, (w - fw) / 2), my = Math.max(0, (hh - fh) / 2);
    tx = Math.min(mx, Math.max(-mx, tx));
    ty = Math.min(my, Math.max(-my, ty));
  };
  const paint = () => {
    img.style.width = `${iw * base * k}px`;
    img.style.height = `${ih * base * k}px`;
    img.style.transform = `translate(calc(-50% + ${tx}px), calc(-50% + ${ty}px))`;
  };
  const setZoom = (nk: number) => {
    k = Math.min(4, Math.max(1, nk));
    zoom.value = String(k);
    clamp();
    paint();
  };
  requestAnimationFrame(layout);
  const ro = new ResizeObserver(layout);
  ro.observe(stage);

  // перетаскивание и щипок
  const pts = new Map<number, { x: number; y: number }>();
  let pinch = 0;
  stage.addEventListener('pointerdown', (e) => {
    stage.setPointerCapture(e.pointerId);
    pts.set(e.pointerId, { x: e.clientX, y: e.clientY });
  });
  stage.addEventListener('pointermove', (e) => {
    const p = pts.get(e.pointerId);
    if (!p) return;
    if (pts.size === 2) {
      const [a, b] = [...pts.values()];
      const d0 = Math.hypot(a.x - b.x, a.y - b.y);
      pts.set(e.pointerId, { x: e.clientX, y: e.clientY });
      const [c, d] = [...pts.values()];
      const d1 = Math.hypot(c.x - d.x, c.y - d.y);
      if (pinch && d0) setZoom(k * (d1 / d0));
      pinch = 1;
      return;
    }
    tx += e.clientX - p.x;
    ty += e.clientY - p.y;
    pts.set(e.pointerId, { x: e.clientX, y: e.clientY });
    clamp();
    paint();
  });
  const drop = (e: PointerEvent) => {
    pts.delete(e.pointerId);
    if (pts.size < 2) pinch = 0;
  };
  stage.addEventListener('pointerup', drop);
  stage.addEventListener('pointercancel', drop);
  stage.addEventListener('wheel', (e) => {
    e.preventDefault();
    setZoom(k * (e.deltaY < 0 ? 1.08 : 1 / 1.08));
  }, { passive: false });
  stage.addEventListener('keydown', (e) => {
    const step = 12;
    const m: Record<string, () => void> = {
      ArrowLeft: () => (tx += step), ArrowRight: () => (tx -= step), ArrowUp: () => (ty += step), ArrowDown: () => (ty -= step),
      '+': () => setZoom(k * 1.1), '=': () => setZoom(k * 1.1), '-': () => setZoom(k / 1.1),
    };
    if (!m[e.key]) return;
    e.preventDefault();
    m[e.key]();
    clamp();
    paint();
  });
  zoom.addEventListener('input', () => setZoom(Number(zoom.value)));

  return new Promise((resolve) => {
    const done = (v: Crop | 'cancel') => {
      ro.disconnect();
      dlg.close();
      dlg.remove();
      URL.revokeObjectURL(url);
      resolve(v);
    };
    cancel.addEventListener('click', () => done('cancel'));
    dlg.addEventListener('cancel', (e) => {
      e.preventDefault();
      done('cancel');
    });
    ok.addEventListener('click', () => {
      // рамка в координатах исходного кадра
      const s = base * k;
      const left = (iw * s) / 2 - tx - fw / 2;
      const top = (ih * s) / 2 - ty - fh / 2;
      const r = (v: number) => Math.min(1, Math.max(0, Math.round(v * 10000) / 10000));
      const x = r(left / s / iw), y = r(top / s / ih);
      done({ x, y, w: Math.min(1 - x, r(fw / s / iw)), h: Math.min(1 - y, r(fh / s / ih)) });
    });
    stage.focus();
  });
}
