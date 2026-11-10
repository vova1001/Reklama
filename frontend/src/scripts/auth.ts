// Вход (Telegram-бот, Google, почта с кодом), анкета и выход. Без библиотек; все запросы — с CSRF-токеном, ошибки — понятным языком рядом с полем.
import type { components } from '../lib/api.gen';

type ApiError = components['schemas']['Error']['error'];
type LoginResult = components['schemas']['LoginResult'];
type CodeSent = components['schemas']['CodeSent'];
type BotLink = components['schemas']['BotLink'];
type BotStatus = components['schemas']['BotStatus'];
type Res<T> = { ok: true; status: number; data: T } | { ok: false; status: number; error: ApiError };

const NET_ERROR: ApiError = { code: 'network', message: 'Нет связи с сервером. Проверьте интернет и попробуйте ещё раз.' };

// ===== API =====
let csrf = '';
async function csrfToken(force = false): Promise<string> {
  if (csrf && !force) return csrf;
  const res = await fetch('/api/auth/config', { credentials: 'same-origin', signal: AbortSignal.timeout(10_000) });
  if (!res.ok) throw new Error(String(res.status));
  csrf = ((await res.json()) as { csrfToken: string }).csrfToken;
  return csrf;
}

async function api<T>(method: 'POST' | 'PUT', path: string, body?: unknown, retried = false, timeout = 15_000): Promise<Res<T>> {
  try {
    const res = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': await csrfToken() },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(timeout),
    });
    if (res.status === 204) return { ok: true, status: 204, data: undefined as T };
    const json = await res.json().catch(() => null);
    if (res.ok) return { ok: true, status: res.status, data: json as T };
    const error: ApiError = json?.error ?? { code: 'internal', message: 'Что-то пошло не так. Попробуйте позже.' };
    const ra = Number(res.headers.get('Retry-After'));
    if (ra > 0 && !error.retryAfter) error.retryAfter = ra;
    // токен устарел (например, cookie очистили) — один раз берём новый и повторяем
    if (res.status === 403 && error.code === 'csrf' && !retried) {
      await csrfToken(true);
      return api<T>(method, path, body, true, timeout);
    }
    return { ok: false, status: res.status, error };
  } catch {
    return { ok: false, status: 0, error: NET_ERROR };
  }
}

// ===== помощники интерфейса =====
const $ = <T extends Element = HTMLElement>(sel: string, root: ParentNode = document) => root.querySelector<T>(sel);
const $$ = <T extends Element = HTMLElement>(sel: string, root: ParentNode = document) => [...root.querySelectorAll<T>(sel)];

function setErr(root: ParentNode, field: string, msg: string) {
  const el = $(`[data-err="${field}"]`, root);
  if (el) el.textContent = msg;
  const input = field === 'email' ? $('#email', root) : field === 'name' ? $('#name', root) : field === 'city' ? $('#city', root) : null;
  input?.setAttribute('aria-invalid', msg ? 'true' : 'false');
}
function clearErrs(root: ParentNode) {
  $$('[data-err]', root).forEach((e) => (e.textContent = ''));
  $$('[aria-invalid]', root).forEach((e) => e.setAttribute('aria-invalid', 'false'));
}
function showFields(root: ParentNode, e: ApiError) {
  for (const [k, v] of Object.entries(e.fields ?? {})) setErr(root, k, v);
  if (!e.fields || !Object.keys(e.fields).length) setErr(root, 'form', e.message);
}
function busy(btn: HTMLButtonElement | null, on: boolean) {
  if (!btn) return;
  btn.disabled = on;
  btn.classList.toggle('is-busy', on);
  btn.setAttribute('aria-busy', String(on));
}
const mmss = (s: number) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;

/** Обратный отсчёт: tick(осталось) каждую секунду, done() в конце. Возвращает отмену. */
function countdown(sec: number, tick: (left: number) => void, done: () => void): () => void {
  const end = Date.now() + sec * 1000;
  let t = 0;
  const step = () => {
    const left = Math.max(0, Math.ceil((end - Date.now()) / 1000));
    if (left <= 0) return done();
    tick(left);
    t = window.setTimeout(step, 250);
  };
  step();
  return () => clearTimeout(t);
}

// Переключатель-радиогруппа: щелчок, стрелки, Home/End; onChange(значение)
function radioGroup(group: HTMLElement, onChange?: (v: string) => void) {
  const items = $$<HTMLButtonElement>('[role="radio"]', group);
  const pick = (b: HTMLButtonElement, focus = false) => {
    items.forEach((i) => {
      i.setAttribute('aria-checked', String(i === b));
      i.tabIndex = i === b ? 0 : -1;
    });
    if (focus) b.focus();
    onChange?.(b.dataset.role!);
  };
  items.forEach((b, i) => {
    b.addEventListener('click', () => pick(b));
    b.addEventListener('keydown', (e) => {
      const n = items.length;
      const to = { ArrowRight: i + 1, ArrowDown: i + 1, ArrowLeft: i - 1, ArrowUp: i - 1, Home: 0, End: n - 1 }[e.key];
      if (to === undefined) return;
      e.preventDefault();
      pick(items[(to + n) % n], true);
    });
  });
  return () => items.find((i) => i.getAttribute('aria-checked') === 'true')?.dataset.role ?? '';
}

// ===== вход: способы, почта, код, Telegram =====
// Согласие — одна строка под кнопками («Продолжая, вы принимаете…»): нажатие способа входа и есть согласие.
function initLogin(root: HTMLElement) {
  const panes = $$('[data-pane]', root).filter((p) => p.dataset.pane !== 'profile');
  const start = $('[data-pane="start"]', root);
  if (!start) return;
  const pane = (name: string) => $(`[data-pane="${name}"]`, root)!;
  const show = (name: string, focus?: HTMLElement | null) => {
    panes.forEach((p) => (p.hidden = p.dataset.pane !== name));
    (focus ?? $<HTMLElement>('.pane-t', pane(name)))?.focus();
  };
  const role = (() => {
    const g = $('[data-roles]', start);
    return g ? radioGroup(g) : () => '';
  })();
  const consent = true;

  // Google: обычный переход, роль — в адрес
  $<HTMLAnchorElement>('[data-google]', start)?.addEventListener('click', (e) => {
    e.preventDefault();
    const q = new URLSearchParams({ consent: '1' });
    if (role()) q.set('role', role());
    location.assign(`/api/auth/google/start?${q}`);
  });

  // ----- Telegram -----
  const tgBtn = $<HTMLButtonElement>('[data-telegram]', start);
  const botId = root.dataset.telegramBot ?? '';
  const mode = root.dataset.telegramMode;
  let widgetLoaded = false;
  const loadWidget = () => {
    if (widgetLoaded || !botId) return;
    widgetLoaded = true;
    const s = document.createElement('script');
    s.src = 'https://telegram.org/js/telegram-widget.js?22';
    s.async = true;
    document.head.append(s);
  };
  // окно Telegram Login — запасной способ (нужен /setdomain у бота)
  const widget = (btn: HTMLButtonElement | null, errRoot: HTMLElement) => {
    const tg = (window as unknown as { Telegram?: { Login?: { auth: (o: object, cb: (u: unknown) => void) => void } } }).Telegram;
    if (!tg?.Login) {
      setErr(errRoot, errRoot === start ? 'form' : 'tg', 'Окно Telegram не загрузилось. Попробуйте через бота или другим способом.');
      return;
    }
    busy(btn, true);
    tg.Login.auth({ bot_id: botId, request_access: false, lang: 'ru' }, async (user) => {
      if (!user) {
        busy(btn, false);
        return;
      }
      const r = await api<LoginResult>('POST', '/api/auth/telegram', { user, role: role(), consent });
      if (r.ok) return location.assign(r.data.next);
      busy(btn, false);
      setErr(errRoot, errRoot === start ? 'form' : 'tg', r.error.message);
    });
  };

  const tgPane = $('[data-pane="tg"]', root);
  const tgOpen = tgPane ? $<HTMLAnchorElement>('[data-tg-open]', tgPane) : null;
  const tgWait = tgPane ? $('[data-tg-wait]', tgPane) : null;
  const tgWaitText = tgPane ? $('[data-tg-wait-text]', tgPane) : null;
  const tgExpired = tgPane ? $('[data-tg-expired]', tgPane) : null;
  let gen = 0; // поколение ожидания: «Назад» или новая ссылка останавливают прежнее
  let stopTimer: (() => void) | null = null;

  const tgStop = () => {
    gen++;
    stopTimer?.();
  };
  const tgFail = (msg: string) => {
    tgStop();
    if (!tgPane) return;
    tgWait!.hidden = true;
    tgOpen!.hidden = true;
    tgExpired!.hidden = false;
    setErr(tgPane, 'tg', msg);
  };

  async function poll(my: number) {
    let pause = 0;
    while (my === gen) {
      if (pause) await new Promise((r) => setTimeout(r, pause));
      if (my !== gen) return;
      const r = await api<BotStatus>('POST', '/api/auth/telegram/bot/status', undefined, false, 40_000);
      if (my !== gen) return;
      if (r.ok) {
        pause = 0;
        if (r.data.status === 'ok') {
          tgWaitText!.textContent = 'Вход подтверждён, открываем кабинет…';
          tgStop();
          return location.assign(r.data.next ?? '/account');
        }
        continue;
      }
      const e = r.error;
      if (e.code === 'login_expired' || e.code === 'login_cancelled') return tgFail(e.message);
      // сеть, лимит, сбой сервера — ждём и пробуем снова, пока ссылка жива
      pause = e.retryAfter ? e.retryAfter * 1000 : 3000;
    }
  }

  async function tgStart(btn: HTMLButtonElement) {
    clearErrs(start!);
    // компьютер: вкладку открываем сразу по нажатию (иначе браузер заблокирует окно), адрес — после ответа сервера
    const fine = matchMedia('(pointer: fine)').matches;
    const pop = fine ? window.open('about:blank', '_blank') : null;
    busy(btn, true);
    const r = await api<BotLink>('POST', '/api/auth/telegram/bot/start', { role: role(), consent });
    busy(btn, false);
    if (!r.ok) {
      pop?.close();
      const errRoot = tgPane && !tgPane.hidden ? tgPane : start!;
      setErr(errRoot, errRoot === start ? 'form' : 'tg', r.error.message);
      return;
    }
    tgStop();
    const my = gen;
    tgOpen!.href = r.data.url;
    tgOpen!.hidden = false;
    tgWait!.hidden = false;
    tgExpired!.hidden = true;
    clearErrs(tgPane!);
    if (pop) {
      pop.opener = null;
      pop.location.href = r.data.url;
    }
    show('tg');
    stopTimer = countdown(
      r.data.expiresIn,
      (left) => (tgWaitText!.textContent = `Ждём подтверждения · ${mmss(left)}`),
      () => tgFail('Время вышло — ссылка больше не действует.'),
    );
    void poll(my);
  }

  if (tgBtn) {
    if (mode === 'widget') loadWidget();
    tgBtn.addEventListener('click', () => (mode === 'bot' ? tgStart(tgBtn) : widget(tgBtn, start)));
  }
  if (tgPane) {
    const again = $<HTMLButtonElement>('[data-tg-again]', tgPane)!;
    again.addEventListener('click', () => tgStart(again));
    const w = $<HTMLButtonElement>('[data-tg-widget]', tgPane);
    w?.addEventListener('pointerenter', loadWidget, { once: true });
    w?.addEventListener('focus', loadWidget, { once: true });
    w?.addEventListener('click', () => {
      loadWidget();
      // скрипт мог ещё грузиться — дадим ему секунду
      const t = Date.now();
      const tryOpen = () => {
        const ok = (window as unknown as { Telegram?: { Login?: unknown } }).Telegram?.Login;
        if (ok || Date.now() - t > 1500) return widget(w, tgPane);
        setTimeout(tryOpen, 100);
      };
      tryOpen();
    });
  }

  // ----- почта → код -----
  const emailPane = $('[data-pane="email"]', root);
  const codePane = $('[data-pane="code"]', root);
  $$('[data-back]', root).forEach((b) =>
    b.addEventListener('click', () => {
      tgStop();
      show('start', $<HTMLElement>('h1', start));
    }),
  );
  $('[data-to-email]', start)?.addEventListener('click', () => {
    clearErrs(start);
    show('email', $<HTMLElement>('#email', emailPane!));
  });
  if (!emailPane || !codePane) return;
  const ep: HTMLElement = emailPane;
  const cp: HTMLElement = codePane;
  const form = $<HTMLFormElement>('[data-email-form]', ep);
  const email = $<HTMLInputElement>('#email', ep);
  const sendBtn = $<HTMLButtonElement>('[data-submit]', ep);
  const sendLabel = sendBtn?.querySelector('span');
  let sentTo = '';
  let stopSendWait: (() => void) | null = null;

  // лимит 429 на кнопке «Получить код»: «Повторить через 0:42»
  const lockSend = (sec: number) => {
    stopSendWait?.();
    if (!sendBtn || !sendLabel) return;
    sendBtn.disabled = true;
    stopSendWait = countdown(
      sec,
      (left) => (sendLabel.textContent = `Повторить через ${mmss(left)}`),
      () => {
        sendBtn.disabled = false;
        sendLabel.textContent = 'Получить код';
      },
    );
  };

  async function send(addr: string, btn: HTMLButtonElement | null): Promise<CodeSent | null> {
    busy(btn, true);
    const r = await api<CodeSent>('POST', '/api/auth/email/start', { email: addr, role: role(), consent });
    busy(btn, false);
    if (r.ok) return r.data;
    return Promise.reject(r.error);
  }

  form?.addEventListener('submit', async (e) => {
    e.preventDefault();
    clearErrs(ep);
    const v = email!.value.trim();
    if (!v) return setErr(ep, 'email', 'Введите почту'), email!.focus();
    if (!/^[^\s@<>"]+@[^\s@<>"]+\.[^\s@<>"]+$/.test(v)) return setErr(ep, 'email', 'Проверьте адрес почты — например, name@mail.ru'), email!.focus();
    try {
      const sent = await send(v, sendBtn);
      if (sent) openCode(sent);
    } catch (err) {
      const er = err as ApiError;
      showFields(ep, er);
      if (er.code === 'rate_limited' && er.retryAfter) lockSend(er.retryAfter);
      if (er.fields?.email) email!.focus();
    }
  });

  // ----- код -----
  const digits = $$<HTMLInputElement>('[data-digit]', cp);
  const codeForm = $<HTMLFormElement>('[data-code-form]', cp)!;
  const verifyBtn = $<HTMLButtonElement>('[data-verify]', cp);
  const resendBtn = $<HTMLButtonElement>('[data-resend]', cp)!;
  const resendWait = $('[data-resend-wait]', cp)!;
  let stopResend: (() => void) | null = null;
  let verifying = false;

  const startResendTimer = (sec: number) => {
    stopResend?.();
    resendBtn.hidden = true;
    stopResend = countdown(
      sec,
      (left) => (resendWait.textContent = `Отправить ещё раз можно через ${mmss(left)}`),
      () => {
        resendWait.textContent = '';
        resendBtn.hidden = false;
      },
    );
  };

  const code = () => digits.map((d) => d.value).join('');
  const fill = (s: string, from = 0) => {
    const chars = s.replace(/\D/g, '').slice(0, 6 - from).split('');
    chars.forEach((c, i) => (digits[from + i].value = c));
    const next = Math.min(from + chars.length, 5);
    digits[next].focus();
    if (code().length === 6) codeForm.requestSubmit();
  };
  const markCode = (bad: boolean) => digits.forEach((d) => d.setAttribute('aria-invalid', String(bad)));
  const resetCode = () => {
    digits.forEach((d) => (d.value = ''));
    digits[0].focus();
  };

  digits.forEach((d, i) => {
    d.addEventListener('input', () => {
      markCode(false);
      setErr(cp, 'code', '');
      const v = d.value.replace(/\D/g, '');
      d.value = '';
      if (v) fill(v, i);
    });
    d.addEventListener('keydown', (e) => {
      if (e.key === 'Backspace' && !d.value && i > 0) {
        digits[i - 1].value = '';
        digits[i - 1].focus();
        e.preventDefault();
      } else if (e.key === 'ArrowLeft' && i > 0) digits[i - 1].focus();
      else if (e.key === 'ArrowRight' && i < 5) digits[i + 1].focus();
    });
    d.addEventListener('paste', (e) => {
      e.preventDefault();
      fill(e.clipboardData?.getData('text') ?? '', i);
    });
    d.addEventListener('focus', () => d.select());
  });

  function openCode(sent: CodeSent) {
    sentTo = sent.email;
    $('[data-sent-to]', cp)!.textContent = sent.email;
    clearErrs(cp);
    markCode(false);
    digits.forEach((d) => ((d.value = ''), (d.disabled = false)));
    if (verifyBtn) verifyBtn.disabled = false;
    startResendTimer(sent.resendAfter);
    show('code', digits[0]);
  }

  codeForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (verifying) return;
    clearErrs(cp);
    const c = code();
    if (c.length !== 6) {
      markCode(true);
      setErr(cp, 'code', 'Введите все 6 цифр из письма');
      digits[Math.min(c.length, 5)].focus();
      return;
    }
    verifying = true;
    busy(verifyBtn, true);
    const r = await api<LoginResult>('POST', '/api/auth/email/verify', { email: sentTo, code: c });
    if (r.ok) {
      if (verifyBtn) verifyBtn.querySelector('span')!.textContent = 'Входим…';
      location.assign(r.data.next);
      return;
    }
    verifying = false;
    busy(verifyBtn, false);
    markCode(true);
    const er = r.error;
    if (er.code === 'code_expired' || er.code === 'too_many_attempts') {
      setErr(cp, 'code', er.message);
      stopResend?.();
      resendWait.textContent = '';
      resendBtn.hidden = false; // новый код можно запросить сразу (с учётом лимита сервера)
      digits.forEach((d) => (d.disabled = true));
      if (verifyBtn) verifyBtn.disabled = true;
      resendBtn.focus();
      return;
    }
    setErr(cp, 'code', er.message);
    if (er.code === 'rate_limited' && er.retryAfter) {
      digits.forEach((d) => (d.disabled = true));
      if (verifyBtn) verifyBtn.disabled = true;
      countdown(er.retryAfter, (left) => setErr(cp, 'code', `Слишком много попыток. Подождите ${mmss(left)}`), () => {
        digits.forEach((d) => (d.disabled = false));
        if (verifyBtn) verifyBtn.disabled = false;
        setErr(cp, 'code', '');
        resetCode();
      });
      return;
    }
    if (er.code === 'network') return;
    resetCode();
  });

  resendBtn.addEventListener('click', async () => {
    clearErrs(cp);
    try {
      const sent = await send(sentTo, resendBtn);
      if (!sent) return;
      digits.forEach((d) => (d.disabled = false));
      if (verifyBtn) verifyBtn.disabled = false;
      markCode(false);
      resendWait.textContent = 'Новый код отправлен.';
      digits.forEach((d) => (d.value = ''));
      setTimeout(() => startResendTimer(sent.resendAfter), 1200);
      digits[0].focus();
    } catch (err) {
      const er = err as ApiError;
      setErr(cp, 'code', er.message);
      if (er.code === 'rate_limited' && er.retryAfter) startResendTimer(er.retryAfter);
    }
  });

  $('[data-back-email]', cp)?.addEventListener('click', () => {
    stopResend?.();
    clearErrs(ep);
    show('email', email);
  });
}

// ===== анкета =====
function initProfile(root: HTMLElement) {
  const form = $<HTMLFormElement>('[data-profile-form]', root);
  if (!form) return;
  let role = form.dataset.role ?? '';
  const nameLabel = $('[data-name-label]', form)!;
  const show = (r: string) => {
    role = r;
    $$('[data-for]', form).forEach((el) => (el.hidden = el.dataset.for !== r));
    nameLabel.textContent = r === 'creator' ? 'Имя или псевдоним' : 'Название компании или бренда';
    setErr(form, 'role', '');
  };
  const g = $('[data-roles]', form);
  if (g) radioGroup(g, show);

  // сферы — пилюли-переключатели, до 5
  const chips = $$<HTMLButtonElement>('[data-slug]', form);
  chips.forEach((c) =>
    c.addEventListener('click', () => {
      const on = c.getAttribute('aria-pressed') === 'true';
      if (!on && chips.filter((x) => x.getAttribute('aria-pressed') === 'true').length >= 5) {
        setErr(form, 'spheres', 'Можно выбрать до 5 сфер');
        return;
      }
      setErr(form, 'spheres', '');
      c.setAttribute('aria-pressed', String(!on));
    }),
  );

  // каналы — до 5 полей
  const list = $('[data-channels]', form)!;
  const add = $<HTMLButtonElement>('[data-add-channel]', form)!;
  const rows = () => $$('.chan', list);
  const syncAdd = () => (add.hidden = rows().length >= 5);
  add.addEventListener('click', () => {
    const tpl = rows()[0];
    const row = tpl.cloneNode(true) as HTMLElement;
    const input = $<HTMLInputElement>('input', row)!;
    input.value = '';
    input.setAttribute('aria-label', `Канал ${rows().length + 1}`);
    const rm = document.createElement('button');
    rm.type = 'button';
    rm.className = 'rm';
    rm.setAttribute('aria-label', 'Убрать канал');
    rm.innerHTML = '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg>';
    rm.addEventListener('click', () => {
      row.remove();
      syncAdd();
      add.focus();
    });
    row.append(rm);
    list.append(row);
    syncAdd();
    input.focus();
  });

  const btn = $<HTMLButtonElement>('[data-submit]', form);
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    clearErrs(form);
    const name = $<HTMLInputElement>('#name', form)!.value.trim();
    const city = $<HTMLInputElement>('#city', form)!.value.trim();
    const spheres = chips.filter((c) => c.getAttribute('aria-pressed') === 'true').map((c) => c.dataset.slug!);
    const channels = $$<HTMLInputElement>('.chan input', list).map((i) => i.value.trim()).filter(Boolean);
    let bad = false;
    const need = (cond: boolean, f: string, msg: string) => {
      if (cond) {
        setErr(form, f, msg);
        bad = true;
      }
    };
    need(!role, 'role', 'Выберите: бизнес или автор');
    need(name.length < 2, 'name', role === 'creator' ? 'Как вас называть — от 2 символов' : 'Название — от 2 символов');
    need(city.length < 2, 'city', 'Укажите город');
    need(role === 'business' && spheres.length === 0, 'spheres', 'Выберите хотя бы одну сферу');
    need(role === 'creator' && channels.length === 0, 'channels', 'Добавьте хотя бы одну ссылку на канал');
    if (bad) {
      ($('[aria-invalid="true"]', form) ?? $('[role="radio"]', form))?.focus();
      return;
    }
    busy(btn, true);
    const r = await api<LoginResult>('PUT', '/api/me/profile', { role, name, city, spheres, channels });
    if (r.ok) return location.assign(r.data.next);
    busy(btn, false);
    if (r.status === 401) return location.assign('/login');
    showFields(form, r.error);
  });
}

// ===== выход =====
function initLogout() {
  const btn = $<HTMLButtonElement>('[data-logout]');
  btn?.addEventListener('click', async () => {
    busy(btn, true);
    const r = await api<void>('POST', '/api/auth/logout');
    if (r.ok) return location.assign('/');
    busy(btn, false);
    setErr(document, 'form', r.error.message);
  });
}

const root = $('[data-auth]');
if (root) {
  initLogin(root);
  initProfile(root);
  initLogout();
  // сообщение об ошибке из адреса (?error=…) убираем из адресной строки, чтобы не всплывало при обновлении
  if (new URLSearchParams(location.search).has('error')) history.replaceState(null, '', location.pathname);
}
