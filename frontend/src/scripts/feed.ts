// Лента: отбор (свои списки с несколькими отметками: щелчок, стрелки, Enter/пробел, Escape; город — Enter или уход
// с поля) и следующие страницы по курсору — без перезагрузки (готовая разметка с сервера: /app/more), сами, когда
// «Показать ещё» близко к экрану.
import { $, $$ } from '../lib/client';

const form = $<HTMLFormElement>('[data-filters]');
if (form) {
  const go = () => {
    const p = new URLSearchParams();
    for (const [k, v] of new FormData(form)) if (String(v).trim()) p.append(k, String(v).trim());
    const q = p.toString();
    location.assign(q ? `/app?${q}` : '/app');
  };
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    go();
  });
  const city = $<HTMLInputElement>('[name="city"]', form)!;
  const initial = city.value;
  city.addEventListener('change', () => city.value.trim() !== initial && go());
  $('[data-clear-city]', form)?.addEventListener('click', (e) => {
    e.preventDefault();
    city.value = '';
    go();
  });

  // Списки с несколькими отметками: щелчок, пробел и Enter отмечают; «Все …» снимает отметки. Закрыли список
  // (Escape, Tab, щелчок мимо, повторно по кнопке) — лента открывается с новым отбором, если он изменился.
  $$<HTMLElement>('[data-pick]', form).forEach((pk) => {
    const btn = $<HTMLButtonElement>('.pk-btn', pk)!;
    const list = $<HTMLUListElement>('.pk-list', pk)!;
    const vals = $('[data-vals]', pk)!;
    const text = $('[data-text]', pk)!;
    const opts = $$<HTMLLIElement>('[role="option"]', list);
    const max = Number(pk.dataset.max) || 5;
    const label = pk.dataset.label ?? '';
    const chosen = () => opts.filter((o) => o.dataset.v && o.getAttribute('aria-selected') === 'true');
    const sig = () => chosen().map((o) => o.dataset.v).join(',');
    let before = sig();
    let i = 0;
    const hi = (n: number) => {
      i = (n + opts.length) % opts.length;
      opts.forEach((o, j) => o.classList.toggle('is-hi', j === i));
      list.setAttribute('aria-activedescendant', opts[i].id);
      opts[i].scrollIntoView({ block: 'nearest' });
    };
    const render = () => {
      const c = chosen();
      opts[0].setAttribute('aria-selected', String(!c.length));
      vals.replaceChildren(
        ...c.map((o) => {
          const inp = document.createElement('input');
          inp.type = 'hidden';
          inp.name = pk.dataset.name!;
          inp.value = o.dataset.v!;
          return inp;
        }),
      );
      text.textContent = !c.length ? label : c.length === 1 ? c[0].dataset.t! : `${label} · ${c.length}`;
      btn.toggleAttribute('data-on', c.length > 0);
      btn.setAttribute('aria-label', `${label}: ${c.length ? c.map((o) => o.dataset.t).join(', ') : 'все'}`);
    };
    const open = (on: boolean) => {
      if (list.hidden === !on) return;
      list.hidden = !on;
      btn.setAttribute('aria-expanded', String(on));
      if (on) {
        before = sig();
        hi(Math.max(0, opts.findIndex((o) => o.getAttribute('aria-selected') === 'true')));
        list.focus();
      } else if (sig() !== before) go();
    };
    const toggle = (o: HTMLLIElement) => {
      if (!o.dataset.v) opts.forEach((x) => x.setAttribute('aria-selected', 'false'));
      else if (o.getAttribute('aria-selected') === 'true') o.setAttribute('aria-selected', 'false');
      else if (chosen().length >= max) return;
      else o.setAttribute('aria-selected', 'true');
      render();
    };
    btn.addEventListener('click', () => open(list.hidden));
    opts.forEach((o, j) => {
      o.addEventListener('click', () => toggle(o));
      o.addEventListener('pointermove', () => hi(j));
    });
    list.addEventListener('keydown', (e) => {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        hi(i + (e.key === 'ArrowDown' ? 1 : -1));
      } else if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        toggle(opts[i]);
      } else if (e.key === 'Escape' || e.key === 'Tab') {
        if (e.key === 'Escape') btn.focus();
        open(false);
      }
    });
    document.addEventListener('pointerdown', (e) => {
      if (!pk.contains(e.target as Node)) open(false);
    });
  });
}

// ===== следующие страницы =====
const tiles = $('[data-tiles]');
const more = $<HTMLButtonElement>('[data-more]');
if (tiles && more) {
  let loading = false;
  const load = async () => {
    const next = more.dataset.more;
    if (loading || !next) return;
    loading = true;
    more.classList.add('is-busy');
    try {
      const u = new URL('/app/more', location.origin);
      new URLSearchParams(location.search).forEach((v, k) => k !== 'cursor' && u.searchParams.append(k, v));
      u.searchParams.set('kind', tiles.dataset.tiles!);
      u.searchParams.set('cursor', next);
      const res = await fetch(u, { credentials: 'same-origin' });
      if (!res.ok) throw new Error(String(res.status));
      const tpl = document.createElement('template');
      tpl.innerHTML = await res.text();
      const box = tpl.content.querySelector<HTMLElement>('[data-page]');
      tiles.append(...(box ? [...box.children] : []));
      more.dataset.more = box?.dataset.next ?? '';
      more.textContent = 'Показать ещё';
      more.parentElement!.hidden = !more.dataset.more;
    } catch {
      more.textContent = 'Не загрузилось — ещё раз';
    } finally {
      loading = false;
      more.classList.remove('is-busy');
    }
  };
  more.addEventListener('click', () => void load());
  if ('IntersectionObserver' in window) {
    new IntersectionObserver((es) => void (es.some((e) => e.isIntersecting) && load()), { rootMargin: '400px' }).observe(more);
  }
}
