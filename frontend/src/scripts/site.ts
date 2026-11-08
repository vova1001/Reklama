// Поведение сайта без библиотек: шапка, меню, появление при прокрутке, параллакс фото, вопросы.
const reduce = window.matchMedia('(prefers-reduced-motion: reduce)');

// Шапка сворачивается в «пилюлю» после первого экрана и разворачивается у самого верха (с запасом, чтобы не дрожала)
const bar = document.querySelector<HTMLElement>('[data-bar]');
const onScrollBar = () => {
  if (!bar) return;
  const y = window.scrollY;
  if (y > 140) bar.classList.add('is-pill');
  else if (y < 60) bar.classList.remove('is-pill');
};
onScrollBar();
window.addEventListener('scroll', onScrollBar, { passive: true });

// Мобильное меню
const burger = document.querySelector<HTMLButtonElement>('[data-burger]');
const menu = document.querySelector<HTMLElement>('[data-menu]');
const setMenu = (open: boolean) => {
  document.body.classList.toggle('menu-open', open);
  burger?.setAttribute('aria-expanded', String(open));
  burger?.setAttribute('aria-label', open ? 'Закрыть меню' : 'Меню');
};
burger?.addEventListener('click', () => setMenu(!document.body.classList.contains('menu-open')));
menu?.addEventListener('click', (e) => {
  if ((e.target as HTMLElement).closest('a')) setMenu(false);
});
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && document.body.classList.contains('menu-open')) {
    setMenu(false);
    burger?.focus();
  }
});
window.matchMedia('(min-width: 1200px)').addEventListener('change', (m) => m.matches && setMenu(false));

// Появление блоков при прокрутке (fade + сдвиг; у карточек — по очереди через --i)
const items = document.querySelectorAll<HTMLElement>('[data-reveal]');
if ('IntersectionObserver' in window && !reduce.matches) {
  const io = new IntersectionObserver(
    (entries) => {
      for (const e of entries) {
        if (e.isIntersecting) {
          e.target.classList.add('is-in');
          io.unobserve(e.target);
        }
      }
    },
    { rootMargin: '0px 0px -8% 0px', threshold: 0.12 },
  );
  items.forEach((el) => io.observe(el));
} else {
  items.forEach((el) => el.classList.add('is-in'));
}

// Лёгкий параллакс и масштаб у фото первого экрана
const pics = document.querySelector<HTMLElement>('[data-parallax]');
if (pics && !reduce.matches) {
  const imgs = [...pics.querySelectorAll<HTMLImageElement>('img')];
  let ticking = false;
  const update = () => {
    ticking = false;
    const r = pics.getBoundingClientRect();
    if (r.bottom < 0 || r.top > window.innerHeight) return;
    const p = Math.min(Math.max(window.scrollY / Math.max(r.bottom + window.scrollY, 1), 0), 1);
    imgs.forEach((img, i) => {
      const k = i % 2 ? 1 : 0.7; // соседние фото — с разной скоростью
      img.style.transform = p ? `translate3d(0, ${(-14 * p * k).toFixed(2)}px, 0) scale(${(1 + 0.07 * p).toFixed(4)})` : '';
    });
  };
  window.addEventListener('scroll', () => {
    if (!ticking) {
      ticking = true;
      requestAnimationFrame(update);
    }
  }, { passive: true });
  update();
}

// Прогресс при прокрутке: --p (0…1) для подсветки мысли по словам и прорисовки линий схем.
// data-progress="начало,конец" — доли высоты экрана, где находится верх элемента в начале и в конце.
const clamp01 = (v: number) => Math.min(Math.max(v, 0), 1);
const progressEls = [...document.querySelectorAll<HTMLElement>('[data-progress]')];
const pxEls = [...document.querySelectorAll<HTMLElement>('[data-px]')];
const zoneNav = document.querySelector<HTMLElement>('[data-zonenav]');
const zones = [...document.querySelectorAll<HTMLElement>('[data-zone]')];
const zoneLinks = new Map([...document.querySelectorAll<HTMLElement>('[data-zone-link]')].map((a) => [a.dataset.zoneLink!, a]));

const lightNodes = (flow: HTMLElement, p: number) => {
  const track = flow.querySelector<HTMLElement>('.track');
  if (!track) return;
  const t = track.getBoundingClientRect();
  const across = t.width > t.height;
  flow.querySelectorAll<HTMLElement>('[data-node]').forEach((n) => {
    const r = n.querySelector('i')!.getBoundingClientRect();
    const pos = across ? (r.left + r.width / 2 - t.left) / t.width : (r.top + r.height / 2 - t.top) / t.height;
    n.classList.toggle('on', p >= pos - 0.02);
  });
};

// вертикальная линия шагов автора заканчивается в центре последнего кружка
const fitTracks = () => document.querySelectorAll<HTMLElement>('.vflow').forEach((f) => {
  const last = f.querySelector<HTMLElement>('[data-node]:last-child');
  if (last) f.style.setProperty('--tb', `${last.offsetHeight - 32}px`);
});
fitTracks();
window.addEventListener('resize', fitTracks);

const updateZones = (vh: number) => {
  if (!zoneNav || zones.length < 2) return;
  const mid = vh * 0.45;
  const [first, last] = [zones[0].getBoundingClientRect(), zones[zones.length - 1].getBoundingClientRect()];
  const inside = first.top < mid && last.bottom > mid;
  zoneNav.classList.toggle('is-on', inside);
  zones.forEach((z) => {
    const r = z.getBoundingClientRect();
    const link = zoneLinks.get(z.dataset.zone!);
    if (!link) return;
    const on = r.top < mid && r.bottom > mid;
    link.classList.toggle('on', on);
    if (on) link.setAttribute('aria-current', 'true');
    else link.removeAttribute('aria-current');
    link.style.setProperty('--p', clamp01((mid - r.top) / r.height).toFixed(3));
  });
};

let queued = false;
const onFrame = () => {
  queued = false;
  const vh = window.innerHeight;
  if (!reduce.matches) {
    for (const el of progressEls) {
      const [a, b] = (el.dataset.progress ?? '0.9,0.4').split(',').map(Number);
      const top = el.getBoundingClientRect().top;
      const p = clamp01((a * vh - top) / ((a - b) * vh));
      el.style.setProperty('--p', p.toFixed(3));
      if (el.hasAttribute('data-flow')) lightNodes(el, p);
    }
    for (const img of pxEls) {
      const r = img.parentElement!.getBoundingClientRect();
      if (r.bottom < 0 || r.top > vh) continue;
      const p = clamp01((vh - r.top) / (vh + r.height));
      img.style.transform = `translate3d(0, ${((0.5 - p) * 48).toFixed(1)}px, 0)`;
    }
  }
  updateZones(vh);
};
const queue = () => {
  if (!queued) {
    queued = true;
    requestAnimationFrame(onFrame);
  }
};
if (reduce.matches) document.querySelectorAll('[data-node]').forEach((n) => n.classList.add('on'));
window.addEventListener('scroll', queue, { passive: true });
window.addEventListener('resize', queue);
onFrame();

// «Своё оформление»: цвет акцента меняется сам, пока превью на экране; после нажатия — только по выбору человека
const brand = document.querySelector<HTMLElement>('[data-brand]');
if (brand) {
  const btns = [...brand.querySelectorAll<HTMLButtonElement>('[data-acc]')];
  let idx = 0;
  let timer = 0;
  let manual = false;
  const pick = (i: number) => {
    idx = i;
    brand.style.setProperty('--acc', btns[i].dataset.acc!);
    btns.forEach((b, k) => b.setAttribute('aria-pressed', String(k === i)));
  };
  const stop = () => window.clearInterval(timer);
  btns.forEach((b, i) => b.addEventListener('click', () => {
    stop();
    manual = true;
    pick(i);
  }));
  if ('IntersectionObserver' in window && !reduce.matches) {
    new IntersectionObserver(([e]) => {
      stop();
      if (e.isIntersecting && !manual) timer = window.setInterval(() => pick((idx + 1) % btns.length), 2600);
    }, { threshold: 0.4 }).observe(brand);
  }
}

// Вопросы: плавное раскрытие (высота — через grid-template-rows в CSS)
document.querySelectorAll<HTMLElement>('[data-accordion] .q').forEach((q) => {
  const btn = q.querySelector<HTMLButtonElement>('button.h');
  btn?.addEventListener('click', () => {
    const open = !q.classList.contains('q-on');
    q.classList.toggle('q-on', open);
    btn.setAttribute('aria-expanded', String(open));
  });
});
