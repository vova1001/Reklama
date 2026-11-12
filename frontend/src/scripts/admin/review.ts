// Карточка проверки: взять в работу (аренда 10 минут, продление каждые 4 минуты), решения с горячими клавишами
// (A — опубликовать, R — отклонить, D — на доработку, J — следующий, K — назад), после решения — сразу следующий
// в очереди по тому же отбору; решение можно отменить 10 с (полоса внизу на следующей странице).
import { $, $$, api, busy, clearErrs, setErr, showFields } from '../../lib/client';
import { guard, toast } from '../../lib/ui';
import { rememberUndo } from './undo';

const root = $('[data-review]');
if (root) {
  const kind = root.dataset.kind!;
  const id = root.dataset.id!;
  const base = `/api/admin/review/${kind}/${encodeURIComponent(id)}`;
  const lease = $('[data-lease]');
  const decide = $('[data-decide]');
  let held = false;
  let deciding = false;

  const filter = (): Record<string, unknown> => {
    try {
      return JSON.parse(sessionStorage.getItem('adm-filter') ?? '{}');
    } catch {
      return {};
    }
  };

  /** Следующий по отбору (текущий — отпускается); очередь пуста — в очередь. */
  async function next(skip: boolean) {
    const r = await api<{ type: string; id: string }>('POST', '/api/admin/queue/next', { ...filter(), skipType: skip ? kind : '', skipId: skip ? id : '' });
    if (!r.ok) return guard(r.status) || toast(r.error.message);
    if (r.status === 204) return location.assign('/admin/queue?empty=1');
    location.assign(`/admin/review/${r.data.type}/${encodeURIComponent(r.data.id)}`);
  }

  function lockDecisions(msg: string, err = true) {
    held = false;
    if (lease) {
      lease.textContent = msg;
      lease.classList.toggle('is-err', err);
    }
    $$<HTMLButtonElement>('button, textarea, input', decide ?? undefined).forEach((b) => (b.disabled = true));
  }

  async function claim() {
    if (!root!.dataset.inQueue) return;
    const r = await api<{ expiresAt: string }>('POST', `${base}/claim`);
    if (r.ok) {
      held = true;
      if (lease) {
        lease.textContent = 'В работе у вас — другие модераторы этого не видят.';
        lease.classList.remove('is-err');
      }
      return;
    }
    if (guard(r.status)) return;
    if (r.error.code === 'lease_held') return lockDecisions('Это уже проверяет другой модератор. Откройте следующий (J).');
    if (r.status === 404) return lockDecisions('Уже проверено. Откройте следующий (J).');
    if (r.status === 403) return location.assign(`/admin/2fa?next=${encodeURIComponent(location.pathname)}`);
    lockDecisions(r.error.message);
  }
  void claim();
  const renew = window.setInterval(() => document.visibilityState === 'visible' && held && claim(), 4 * 60_000);
  window.addEventListener('pagehide', () => clearInterval(renew));

  const forms = { reject: $<HTMLFormElement>('[data-form="reject"]'), rework: $<HTMLFormElement>('[data-form="rework"]') };
  function open(which: 'reject' | 'rework') {
    for (const [k, f] of Object.entries(forms)) {
      if (!f) continue;
      f.hidden = k !== which;
      $(`[data-act="${k}"]`)?.setAttribute('aria-expanded', String(k === which));
    }
    const f = forms[which];
    $<HTMLElement>(which === 'reject' ? 'textarea' : 'input', f ?? undefined)?.focus();
  }

  async function send(action: 'approve' | 'reject' | 'rework', body: Record<string, unknown>, form: HTMLFormElement | null, btn: HTMLButtonElement | null) {
    if (deciding || !held) return;
    deciding = true;
    if (form) clearErrs(form);
    busy(btn, true);
    const r = await api<{ id: number; applyAt: string }>('POST', `${base}/decision`, { action, ...body });
    if (!r.ok) {
      deciding = false;
      busy(btn, false);
      if (guard(r.status)) return;
      if (r.error.fields && form) return showFields(form, r.error);
      if (r.error.code === 'lease_lost') {
        toast(r.error.message);
        return claim();
      }
      return toast(r.error.message);
    }
    const title = $('.page-t')?.textContent ?? '';
    rememberUndo({ id: r.data.id, action, title, applyAt: r.data.applyAt, back: location.pathname });
    await next(false);
  }

  $('[data-act="approve"]')?.addEventListener('click', (e) => send('approve', {}, null, e.currentTarget as HTMLButtonElement));
  $('[data-act="reject"]')?.addEventListener('click', () => open('reject'));
  $('[data-act="rework"]')?.addEventListener('click', () => open('rework'));

  // шаблоны причины: выбранные — в текст, свой текст дописывается
  const reason = $<HTMLTextAreaElement>('#rj-reason');
  $$<HTMLButtonElement>('[data-tpl]').forEach((b) =>
    b.addEventListener('click', () => {
      const on = b.getAttribute('aria-pressed') !== 'true';
      b.setAttribute('aria-pressed', String(on));
      if (!reason) return;
      const t = b.dataset.tpl!;
      const parts = reason.value.split(/\.\s+/).map((s) => s.trim().replace(/\.$/, '')).filter(Boolean);
      const next = on ? [...parts.filter((p) => p !== t), t] : parts.filter((p) => p !== t);
      reason.value = next.join('. ');
      reason.focus();
    }),
  );

  forms.reject?.addEventListener('submit', (e) => {
    e.preventDefault();
    const f = forms.reject!;
    const v = reason?.value.trim() ?? '';
    if (v.length < 5) return setErr(f, 'reason', 'Причина — от 5 символов');
    void send('reject', { reason: v }, f, $<HTMLButtonElement>('button[type=submit]', f));
  });
  forms.rework?.addEventListener('submit', (e) => {
    e.preventDefault();
    const f = forms.rework!;
    const notes: Record<string, string> = {};
    $$<HTMLInputElement>('input', f).forEach((i) => i.value.trim() && (notes[i.name] = i.value.trim()));
    if (!Object.keys(notes).length) return setErr(f, 'notes', 'Напишите хотя бы одно замечание');
    void send('rework', { notes }, f, $<HTMLButtonElement>('button[type=submit]', f));
  });

  $('[data-next]')?.addEventListener('click', () => next(true));

  document.addEventListener('keydown', (e) => {
    if (e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented) return;
    const t = e.target as HTMLElement;
    if (t.closest('input, textarea, [contenteditable], dialog[open]')) return;
    // по коду клавиши — работает и в русской раскладке
    const map: Record<string, () => void> = {
      KeyA: () => decide && $<HTMLButtonElement>('[data-act="approve"]')?.click(),
      KeyR: () => decide && open('reject'),
      KeyD: () => decide && open('rework'),
      KeyJ: () => void next(true),
      KeyK: () => history.back(),
    };
    if (map[e.code]) {
      e.preventDefault();
      map[e.code]();
    }
  });
}
