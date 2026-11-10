// Вход (Telegram-бот, Google, почта с кодом). Все запросы — с CSRF-токеном, ошибки — понятным языком рядом с полем.
// После входа — сразу приложение (/app); роль не выбирается: один аккаунт — два режима, переключаются внутри.
import type { components } from '../lib/api.gen';
import { $, $$, api, busy, clearErrs, countdown, mmss, setErr, showFields, type ApiError } from '../lib/client';

type LoginResult = components['schemas']['LoginResult'];
type CodeSent = components['schemas']['CodeSent'];
type BotLink = components['schemas']['BotLink'];
type BotStatus = components['schemas']['BotStatus'];

const APP = '/app';

// ===== вход: способы, почта, код, Telegram =====
// Согласие — одна строка под кнопками («Продолжая, вы принимаете…»): нажатие способа входа и есть согласие.
function initLogin(root: HTMLElement) {
  const panes = $$('[data-pane]', root);
  const start = $('[data-pane="start"]', root);
  if (!start) return;
  const pane = (name: string) => $(`[data-pane="${name}"]`, root)!;
  const show = (name: string, focus?: HTMLElement | null) => {
    panes.forEach((p) => (p.hidden = p.dataset.pane !== name));
    (focus ?? $<HTMLElement>('.pane-t', pane(name)))?.focus();
  };
  const consent = true;

  // Google: обычный переход
  $<HTMLAnchorElement>('[data-google]', start)?.addEventListener('click', (e) => {
    e.preventDefault();
    location.assign(`/api/auth/google/start?${new URLSearchParams({ consent: '1' })}`);
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
      const r = await api<LoginResult>('POST', '/api/auth/telegram', { user, consent });
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
          tgWaitText!.textContent = 'Вход подтверждён, открываем приложение…';
          tgStop();
          return location.assign(r.data.next ?? APP);
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
    const r = await api<BotLink>('POST', '/api/auth/telegram/bot/start', { consent });
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
    const r = await api<CodeSent>('POST', '/api/auth/email/start', { email: addr, consent });
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

const root = $('[data-auth]');
if (root) {
  initLogin(root);
  // сообщение об ошибке из адреса (?error=…) убираем из адресной строки, чтобы не всплывало при обновлении
  if (new URLSearchParams(location.search).has('error')) history.replaceState(null, '', location.pathname);
}
