// Перестановка порядка: перетаскивание за ручку (мышь, палец, перо — pointer events, работает на телефоне) и
// клавиатура на ручке (стрелки). Список — вертикальный или сетка (ближайший по центру). Без библиотек.

export interface SortableOptions {
  item: string; // селектор элемента
  handle: string; // селектор ручки внутри элемента
  grid?: boolean; // сетка (фото) — стрелки влево/вправо тоже
  onChange: () => void;
  label?: (el: HTMLElement) => string; // для сообщения читалке: что переместили
}

const live = (() => {
  let el: HTMLElement | null = null;
  return (msg: string) => {
    if (!el) {
      el = document.createElement('p');
      el.className = 'sr-only';
      el.setAttribute('aria-live', 'polite');
      document.body.append(el);
    }
    el.textContent = msg;
  };
})();

export function sortable(list: HTMLElement, o: SortableOptions) {
  const items = () => [...list.children].filter((c): c is HTMLElement => c instanceof HTMLElement && c.matches(o.item));
  const announce = (el: HTMLElement) => {
    const all = items();
    live(`${o.label?.(el) ?? 'Элемент'}: место ${all.indexOf(el) + 1} из ${all.length}`);
  };

  // ===== мышь и палец =====
  list.addEventListener('pointerdown', (e) => {
    const handle = (e.target as HTMLElement).closest<HTMLElement>(o.handle);
    if (!handle || !list.contains(handle) || e.button !== 0) return;
    const el = handle.closest<HTMLElement>(o.item);
    if (!el || el.parentElement !== list) return;
    e.preventDefault();
    handle.setPointerCapture(e.pointerId);
    el.classList.add('is-drag');
    list.classList.add('is-sorting');
    const startIdx = items().indexOf(el);
    const move = (ev: PointerEvent) => {
      let best: HTMLElement | null = null;
      let bestD = Infinity;
      for (const it of items()) {
        if (it === el) continue;
        const r = it.getBoundingClientRect();
        const dx = o.grid ? ev.clientX - (r.left + r.width / 2) : 0;
        const dy = ev.clientY - (r.top + r.height / 2);
        const d = dx * dx + dy * dy;
        if (d < bestD) {
          bestD = d;
          best = it;
        }
      }
      if (!best) return;
      const r = best.getBoundingClientRect();
      const inside = o.grid
        ? ev.clientX > r.left && ev.clientX < r.right && ev.clientY > r.top && ev.clientY < r.bottom
        : ev.clientY > r.top && ev.clientY < r.bottom;
      if (!inside) return;
      const all = items();
      const after = all.indexOf(best) > all.indexOf(el);
      best.insertAdjacentElement(after ? 'afterend' : 'beforebegin', el);
    };
    const up = () => {
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', up);
      handle.removeEventListener('pointercancel', up);
      el.classList.remove('is-drag');
      list.classList.remove('is-sorting');
      if (items().indexOf(el) !== startIdx) {
        announce(el);
        o.onChange();
      }
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', up);
    handle.addEventListener('pointercancel', up);
  });

  // ===== клавиатура: стрелки на ручке =====
  list.addEventListener('keydown', (e) => {
    const handle = (e.target as HTMLElement).closest<HTMLElement>(o.handle);
    if (!handle) return;
    const el = handle.closest<HTMLElement>(o.item);
    if (!el || el.parentElement !== list) return;
    const back = e.key === 'ArrowUp' || (o.grid && e.key === 'ArrowLeft');
    const fwd = e.key === 'ArrowDown' || (o.grid && e.key === 'ArrowRight');
    if (!back && !fwd) return;
    e.preventDefault();
    const all = items();
    const i = all.indexOf(el);
    const j = back ? i - 1 : i + 1;
    if (j < 0 || j >= all.length) return;
    all[j].insertAdjacentElement(back ? 'beforebegin' : 'afterend', el);
    handle.focus();
    announce(el);
    o.onChange();
  });
}
