// Поведение сайта без библиотек: шапка, меню, появление при прокрутке, параллакс фото, вопросы.
const reduce = window.matchMedia('(prefers-reduced-motion: reduce)');

// Шапка уплотняется после начала прокрутки
const bar = document.querySelector<HTMLElement>('[data-bar]');
const onScrollBar = () => bar?.classList.toggle('is-scrolled', window.scrollY > 8);
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

// Вопросы: плавное раскрытие (высота — через grid-template-rows в CSS)
document.querySelectorAll<HTMLElement>('[data-accordion] .q').forEach((q) => {
  const btn = q.querySelector<HTMLButtonElement>('button.h');
  btn?.addEventListener('click', () => {
    const open = !q.classList.contains('q-on');
    q.classList.toggle('q-on', open);
    btn.setAttribute('aria-expanded', String(open));
  });
});
