// Приложение после входа: меню аватара, переключение режима, поиск-заглушка, формы профиля, выход и выход везде.
// Без библиотек; все запросы — с CSRF-токеном, ошибки — рядом с полем.
import type { components } from '../lib/api.gen';
import { $, $$, api, busy, clearErrs, showFields, setErr } from '../lib/client';

type Me = components['schemas']['Me'];
type Mode = components['schemas']['Mode'];

const app = $('[data-app]');
const STEP_TITLES: Record<Mode, string> = { business: 'Профиль бизнеса', creator: 'Профиль автора' };
const MODE_TITLE: Record<Mode, string> = { business: 'Бизнес', creator: 'Автор' };

// ===== сообщения =====
let toastTimer = 0;
function toast(msg: string) {
  const el = $('[data-toast]');
  if (!el) return;
  el.textContent = msg;
  el.classList.add('is-on');
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => el.classList.remove('is-on'), 3200);
}

/** Не вошёл (сессия истекла или вышли на другом устройстве) — на страницу входа. */
function guard(status: number): boolean {
  if (status === 401) {
    location.assign('/login');
    return true;
  }
  return false;
}

// ===== режим =====
function applyMode(mode: Mode) {
  if (!app) return;
  app.dataset.mode = mode;
  $$<HTMLButtonElement>('[data-mode-value]').forEach((b) => {
    const on = b.dataset.modeValue === mode;
    b.setAttribute('aria-checked', String(on));
    b.tabIndex = on ? 0 : -1;
  });
  $$<HTMLButtonElement>('[data-set-mode]').forEach((b) => b.setAttribute('aria-checked', String(b.dataset.setMode === mode)));
  const t = $('[data-step="profile"] [data-step-title]');
  if (t) t.textContent = STEP_TITLES[mode];
  const a = $<HTMLAnchorElement>('[data-step="profile"] a');
  if (a) a.href = `/app/profile?tab=${mode}`;
}

/** Прогресс и подсказки по ответу /api/me. */
function applyMe(me: Me) {
  const num = $('[data-progress-num]');
  if (num) num.textContent = `${me.completion}%`;
  const bar = $('[data-progress-bar]');
  if (bar) {
    bar.setAttribute('aria-valuenow', String(me.completion));
    const fill = $<HTMLElement>('span', bar);
    if (fill) fill.style.width = `${me.completion}%`;
  }
  for (const st of me.checklist) {
    const li = $(`[data-step="${st.key}"]`);
    if (!li) continue;
    li.dataset.done = String(st.done);
    const s = $('[data-step-state]', li);
    if (s) s.textContent = st.done ? 'готово' : 'не заполнено';
  }
  if (app) {
    app.dataset.businessFilled = String(me.profiles.business);
    app.dataset.creatorFilled = String(me.profiles.creator);
  }
  (['business', 'creator'] as Mode[]).forEach((m) => {
    const n = $(`[data-nudge="${m}"]`);
    if (n && (me.profiles[m] || dismissed(m))) n.hidden = true;
    const dot = $(`[data-filled="${m}"]`);
    dot?.classList.toggle('is-on', me.profiles[m]);
  });
}

let switching = false;
async function setMode(mode: Mode) {
  if (!app || switching || app.dataset.mode === mode) return;
  const prev = app.dataset.mode as Mode;
  switching = true;
  applyMode(mode); // сразу — ответ сервера подтвердит
  const r = await api<Me>('PATCH', '/api/me/mode', { mode });
  switching = false;
  if (r.ok) {
    applyMe(r.data);
    toast(`Режим: ${MODE_TITLE[mode]}`);
    return;
  }
  if (guard(r.status)) return;
  applyMode(prev);
  toast(r.error.message);
}

function initModeSwitch() {
  $$('[data-mode-switch]').forEach((group) => {
    const items = $$<HTMLButtonElement>('[role="radio"]', group);
    items.forEach((b, i) => {
      b.addEventListener('click', () => setMode(b.dataset.modeValue as Mode));
      b.addEventListener('keydown', (e) => {
        const n = items.length;
        const to = { ArrowRight: i + 1, ArrowDown: i + 1, ArrowLeft: i - 1, ArrowUp: i - 1, Home: 0, End: n - 1 }[e.key];
        if (to === undefined) return;
        e.preventDefault();
        const next = items[(to + n) % n];
        next.focus();
        void setMode(next.dataset.modeValue as Mode);
      });
    });
  });
}

// ===== меню аватара =====
function initMenu() {
  const btn = $<HTMLButtonElement>('[data-menu-btn]');
  const menu = $('[data-menu]');
  if (!btn || !menu) return;
  const items = () => $$<HTMLElement>('[role^="menuitem"]', menu);
  const open = (focusFirst = true) => {
    menu.hidden = false;
    btn.setAttribute('aria-expanded', 'true');
    if (focusFirst) items()[0]?.focus();
  };
  const close = (focusBtn = true) => {
    if (menu.hidden) return;
    menu.hidden = true;
    btn.setAttribute('aria-expanded', 'false');
    if (focusBtn) btn.focus();
  };
  btn.addEventListener('click', () => (menu.hidden ? open() : close()));
  btn.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      open();
      if (e.key === 'ArrowUp') items().at(-1)?.focus();
    }
  });
  menu.addEventListener('keydown', (e) => {
    const list = items();
    const i = list.indexOf(document.activeElement as HTMLElement);
    const to = { ArrowDown: i + 1, ArrowUp: i - 1, Home: 0, End: list.length - 1 }[e.key];
    if (to !== undefined) {
      e.preventDefault();
      list[(to + list.length) % list.length].focus();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      close();
    } else if (e.key === 'Tab') {
      close(false);
    }
  });
  document.addEventListener('pointerdown', (e) => {
    if (!menu.hidden && !menu.contains(e.target as Node) && !btn.contains(e.target as Node)) close(false);
  });
  $$<HTMLButtonElement>('[data-set-mode]', menu).forEach((b) =>
    b.addEventListener('click', () => {
      close();
      void setMode(b.dataset.setMode as Mode);
    }),
  );
}

// ===== подтверждение =====
function confirmDialog(title: string, text: string, ok: string): Promise<boolean> {
  const dlg = $<HTMLDialogElement>('[data-confirm]');
  if (!dlg || typeof dlg.showModal !== 'function') return Promise.resolve(true);
  $('[data-confirm-title]', dlg)!.textContent = title;
  $('[data-confirm-text]', dlg)!.textContent = text;
  $('[data-confirm-ok]', dlg)!.textContent = ok;
  dlg.returnValue = '';
  dlg.showModal();
  return new Promise((resolve) => {
    dlg.addEventListener('close', () => resolve(dlg.returnValue === 'ok'), { once: true });
  });
}

// ===== выход =====
function initLogout() {
  $$<HTMLButtonElement>('[data-logout]').forEach((btn) =>
    btn.addEventListener('click', async () => {
      busy(btn, true);
      const r = await api<void>('POST', '/api/auth/logout');
      if (r.ok) return location.assign('/');
      busy(btn, false);
      toast(r.error.message);
    }),
  );
  const all = $<HTMLButtonElement>('[data-logout-all]');
  all?.addEventListener('click', async () => {
    const ok = await confirmDialog('Выйти со всех устройств?', 'Все сеансы завершатся, включая этот. Войти снова можно любым способом.', 'Выйти везде');
    if (!ok) return all.focus();
    busy(all, true);
    const r = await api<{ sessions: number }>('POST', '/api/me/logout-all');
    if (r.ok || r.status === 401) return location.assign('/login');
    busy(all, false);
    setErr(all.closest('section') ?? document, 'form', r.error.message);
  });
}

// ===== поиск (заглушка) =====
function initSearch() {
  $$<HTMLFormElement>('[data-search]').forEach((f) =>
    f.addEventListener('submit', (e) => {
      e.preventDefault();
      toast('Поиск появится вместе с настоящими заданиями — совсем скоро.');
    }),
  );
  const focusSearch = () => {
    if (location.hash !== '#q') return;
    const q = $<HTMLInputElement>('#q');
    if (q && q.offsetParent !== null) q.focus();
    else $<HTMLInputElement>('#q-bar')?.focus();
  };
  focusSearch();
  window.addEventListener('hashchange', focusSearch);
}

// ===== лента: подсказки и примеры =====
const NUDGE_KEY = (m: string) => `ohvat:nudge:${m}`;
function dismissed(m: string): boolean {
  try {
    return sessionStorage.getItem(NUDGE_KEY(m)) === '1';
  } catch {
    return false;
  }
}
function initFeed() {
  $$('[data-nudge]').forEach((n) => {
    const m = n.dataset.nudge!;
    if (dismissed(m)) n.hidden = true;
    $('[data-nudge-close]', n)?.addEventListener('click', () => {
      n.hidden = true;
      try {
        sessionStorage.setItem(NUDGE_KEY(m), '1');
      } catch {
        /* приватный режим — просто скрываем */
      }
    });
  });
  $$<HTMLButtonElement>('[data-respond]').forEach((b) =>
    b.addEventListener('click', () => {
      const note = $('[data-respond-note]', b.parentElement!);
      if (!note) return;
      const needProfile = app?.dataset.creatorFilled !== 'true';
      note.textContent = needProfile
        ? 'Это пример. Чтобы откликаться на настоящие задания, заполните профиль автора.'
        : 'Это пример задания. Настоящие задания появятся здесь, как только бизнес их разместит.';
    }),
  );
}

// ===== профиль: вкладки и формы =====
function initTabs() {
  const list = $('[data-tabs]');
  if (!list) return;
  const tabs = $$<HTMLButtonElement>('[role="tab"]', list);
  const pick = (t: HTMLButtonElement, focus = false) => {
    tabs.forEach((x) => {
      const on = x === t;
      x.setAttribute('aria-selected', String(on));
      x.tabIndex = on ? 0 : -1;
      $(`#${x.getAttribute('aria-controls')}`)!.hidden = !on;
    });
    if (focus) t.focus();
    history.replaceState(null, '', `?tab=${t.dataset.tab}`);
  };
  tabs.forEach((t, i) => {
    t.addEventListener('click', () => pick(t));
    t.addEventListener('keydown', (e) => {
      const n = tabs.length;
      const to = { ArrowRight: i + 1, ArrowLeft: i - 1, Home: 0, End: n - 1 }[e.key];
      if (to === undefined) return;
      e.preventDefault();
      pick(tabs[(to + n) % n], true);
    });
  });
}

function initChips(form: HTMLElement) {
  const chips = $$<HTMLButtonElement>('[data-slug]', form);
  chips.forEach((c) =>
    c.addEventListener('click', () => {
      const on = c.getAttribute('aria-pressed') === 'true';
      if (!on && chips.filter((x) => x.getAttribute('aria-pressed') === 'true').length >= 5) {
        setErr(form, 'spheres', 'Можно выбрать до 5');
        return;
      }
      setErr(form, 'spheres', '');
      c.setAttribute('aria-pressed', String(!on));
    }),
  );
  return () => chips.filter((c) => c.getAttribute('aria-pressed') === 'true').map((c) => c.dataset.slug!);
}

function initChannels(form: HTMLElement) {
  const list = $('[data-channels]', form);
  const add = $<HTMLButtonElement>('[data-add-channel]', form);
  if (!list || !add) return () => [] as string[];
  const rows = () => $$('.chan', list);
  const sync = () => {
    add.hidden = rows().length >= 5;
    rows().forEach((r, i) => $<HTMLInputElement>('input', r)!.setAttribute('aria-label', `Канал ${i + 1}`));
  };
  const bindRemove = (row: HTMLElement) =>
    $('[data-rm]', row)?.addEventListener('click', () => {
      row.remove();
      sync();
      add.focus();
    });
  rows().forEach(bindRemove);
  add.addEventListener('click', () => {
    const row = rows()[0].cloneNode(true) as HTMLElement;
    const input = $<HTMLInputElement>('input', row)!;
    input.value = '';
    input.setAttribute('aria-invalid', 'false');
    $('[data-rm]', row)?.remove();
    const rm = document.createElement('button');
    rm.type = 'button';
    rm.className = 'rm';
    rm.dataset.rm = '';
    rm.setAttribute('aria-label', 'Убрать канал');
    rm.innerHTML = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg>';
    row.append(rm);
    list.append(row);
    bindRemove(row);
    sync();
    input.focus();
  });
  return () => $$<HTMLInputElement>('input', list).map((i) => i.value.trim()).filter(Boolean);
}

type FormKind = 'basics' | 'business' | 'creator';
const ENDPOINT: Record<FormKind, string> = { basics: '/api/me/profile', business: '/api/me/profile/business', creator: '/api/me/profile/creator' };
const SAVED: Record<FormKind, string> = { basics: 'Сохранено', business: 'Профиль бизнеса сохранён', creator: 'Профиль автора сохранён' };

function initForms() {
  $$<HTMLFormElement>('[data-form]').forEach((form) => {
    const kind = form.dataset.form as FormKind;
    const spheres = kind === 'basics' ? () => [] : initChips(form);
    const channels = kind === 'creator' ? initChannels(form) : () => [];
    const btn = $<HTMLButtonElement>('[data-submit]', form);
    const saved = $('[data-saved]', form);
    form.addEventListener('input', () => saved && (saved.textContent = ''));
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      clearErrs(form);
      if (saved) saved.textContent = '';
      const name = $<HTMLInputElement>('[name="name"]', form)!.value.trim();
      const city = $<HTMLInputElement>('[name="city"]', form)!.value.trim();
      const body: Record<string, unknown> = { name, city };
      let bad = false;
      const need = (cond: boolean, f: string, msg: string) => {
        if (cond) {
          setErr(form, f, msg);
          bad = true;
        }
      };
      need(name.length < 2, 'name', kind === 'business' ? 'Название — от 2 символов' : 'Как вас называть — от 2 символов');
      need(city.length < 2, 'city', 'Укажите город');
      if (kind !== 'basics') {
        body.spheres = spheres();
        need((body.spheres as string[]).length === 0, 'spheres', kind === 'business' ? 'Выберите хотя бы одну сферу' : 'Выберите хотя бы одну тему');
      }
      if (kind === 'creator') {
        body.channels = channels();
        need((body.channels as string[]).length === 0, 'channels', 'Добавьте хотя бы одну ссылку на канал');
      }
      if (bad) {
        ($('[aria-invalid="true"]', form) ?? $('[data-err]:not(:empty)', form)?.previousElementSibling as HTMLElement | null)?.focus();
        return;
      }
      busy(btn, true);
      const r = await api<unknown>('PUT', ENDPOINT[kind], body);
      busy(btn, false);
      if (!r.ok) {
        if (guard(r.status)) return;
        showFields(form, r.error);
        return;
      }
      if (saved) saved.textContent = SAVED[kind];
      toast(SAVED[kind]);
      // общие данные — подсказка для пустых полей профилей режимов (имя автора и город)
      if (kind === 'basics') {
        const fill = (sel: string, v: string) => {
          const i = $<HTMLInputElement>(sel);
          if (i && !i.value.trim()) i.value = v;
        };
        fill('[data-form="creator"] [name="name"]', name);
        fill('[data-form="creator"] [name="city"]', city);
        fill('[data-form="business"] [name="city"]', city);
      }
      const me = await api<Me>('GET', '/api/me');
      if (me.ok) applyMe(me.data);
    });
  });
}

if (app) {
  initModeSwitch();
  initMenu();
  initLogout();
  initSearch();
  initFeed();
  initTabs();
  initForms();
}
