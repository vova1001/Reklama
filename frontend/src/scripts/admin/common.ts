// Админка: 2FA, формы действий (блокировка, роль, режим, решения по жалобам, свои категории и форматы),
// «Начать проверку». Запросы — с CSRF; ошибки — рядом с полем или сообщением; опасное — через свой диалог.
import { $, $$, api, busy, clearErrs, setErr, showFields } from '../../lib/client';
import { confirmDialog, guard, toast } from '../../lib/ui';
import './undo';

// ===== 2FA =====
const tfa = $('[data-tfa]');
if (tfa) {
  const next = tfa.dataset.next || '/admin';
  $('[data-tfa-start]')?.addEventListener('click', async (e) => {
    const b = e.currentTarget as HTMLButtonElement;
    busy(b, true);
    const r = await api<{ secret: string; uri: string; qr?: string }>('POST', '/api/admin/2fa/setup');
    busy(b, false);
    if (!r.ok) return guard(r.status) || toast(r.error.message);
    const img = $<HTMLImageElement>('[data-tfa-qr]')!;
    if (r.data.qr) img.src = r.data.qr;
    else img.hidden = true;
    $('[data-tfa-key]')!.textContent = r.data.secret.replace(/(.{4})/g, '$1 ').trim();
    $('[data-step="start"]')!.hidden = true;
    $('[data-step="scan"]')!.hidden = false;
    $<HTMLInputElement>('#tfa-code')?.focus();
  });
  $<HTMLFormElement>('[data-tfa-confirm]')?.addEventListener('submit', async (e) => {
    e.preventDefault();
    const f = e.currentTarget as HTMLFormElement;
    clearErrs(f);
    const code = (f.elements.namedItem('code') as HTMLInputElement).value.trim();
    if (!/^\d{6}$/.test(code.replace(/\s/g, ''))) return setErr(f, 'code', 'Введите 6 цифр из приложения');
    const btn = $<HTMLButtonElement>('button[type=submit]', f);
    busy(btn, true);
    const r = await api<{ backupCodes: string[] }>('POST', '/api/admin/2fa/confirm', { code });
    busy(btn, false);
    if (!r.ok) return guard(r.status) || (r.error.fields ? showFields(f, r.error) : setErr(f, 'code', r.error.message));
    const box = $('[data-tfa-codes]')!;
    box.replaceChildren(...r.data.backupCodes.map((c) => Object.assign(document.createElement('span'), { textContent: c })));
    $('[data-step="scan"]')!.hidden = true;
    $('[data-step="codes"]')!.hidden = false;
  });
  $<HTMLFormElement>('[data-tfa-verify]')?.addEventListener('submit', async (e) => {
    e.preventDefault();
    const f = e.currentTarget as HTMLFormElement;
    clearErrs(f);
    const code = (f.elements.namedItem('code') as HTMLInputElement).value.trim();
    if (!code) return setErr(f, 'code', 'Введите код');
    const btn = $<HTMLButtonElement>('button[type=submit]', f);
    busy(btn, true);
    const r = await api('POST', '/api/admin/2fa/verify', { code });
    if (r.ok) return location.assign(next);
    busy(btn, false);
    if (guard(r.status)) return;
    setErr(f, 'code', r.error.message);
  });
}

// ===== формы действий: data-act="METHOD /api/…", поля → JSON; data-ask — диалог; data-done — что после =====
$$<HTMLFormElement>('form[data-act]').forEach((f) => {
  f.addEventListener('submit', async (e) => {
    e.preventDefault();
    clearErrs(f);
    const sub = (e as SubmitEvent).submitter as HTMLButtonElement | null;
    const [method, path] = (sub?.dataset.act ?? f.dataset.act!).split(' ') as ['POST' | 'PUT', string];
    const body: Record<string, string> = {};
    new FormData(f).forEach((v, k) => (body[k] = String(v)));
    if (sub?.name) body[sub.name] = sub.value;
    const ask = sub?.dataset.ask ?? f.dataset.ask;
    if (ask) {
      const [title, text, ok] = ask.split('|');
      if (!(await confirmDialog(title, text ?? '', ok ?? 'Да'))) return;
    }
    busy(sub, true);
    const r = await api<{ backupCodes?: string[] }>(method, path, Object.keys(body).length ? body : undefined);
    busy(sub, false);
    if (!r.ok) {
      if (guard(r.status)) return;
      if (r.status === 403 && r.error.code.startsWith('2fa')) return location.assign(`/admin/2fa?next=${encodeURIComponent(location.pathname)}`);
      return r.error.fields ? showFields(f, r.error) : toast(r.error.message);
    }
    if (f.dataset.codes !== undefined && r.data?.backupCodes) {
      // новые резервные коды — показать один раз
      const out = $('[data-codes-out]')!;
      out.replaceChildren(...r.data.backupCodes.map((c) => Object.assign(document.createElement('span'), { textContent: c })));
      out.hidden = false;
      return toast('Сохраните новые коды — больше их не покажем');
    }
    const done = sub?.dataset.done ?? f.dataset.done ?? 'reload';
    if (done === 'reload') return location.reload();
    if (done.startsWith('/')) return location.assign(done);
    toast(done);
  });
});

// ===== очередь: «Начать проверку» — взять первый по отбору и открыть =====
$<HTMLButtonElement>('[data-start-review]')?.addEventListener('click', async (e) => {
  const b = e.currentTarget as HTMLButtonElement;
  const f = $<HTMLFormElement>('[data-queue-filter]');
  const fd = f ? new FormData(f) : new FormData();
  const body = {
    type: fd.getAll('type').map(String), category: fd.getAll('category').map(String).filter(Boolean),
    city: String(fd.get('city') ?? ''), q: String(fd.get('q') ?? ''), from: String(fd.get('from') ?? ''), to: String(fd.get('to') ?? ''),
    sort: String(fd.get('sort') ?? 'oldest'),
  };
  sessionStorage.setItem('adm-filter', JSON.stringify(body));
  busy(b, true);
  const r = await api<{ type: string; id: string }>('POST', '/api/admin/queue/next', body);
  busy(b, false);
  if (!r.ok) return guard(r.status) || toast(r.error.message);
  if (r.status === 204) return toast('Очередь пуста');
  location.assign(`/admin/review/${r.data.type}/${encodeURIComponent(r.data.id)}`);
});

// отбор очереди: пилюли-переключатели вида (скрытые чекбоксы)
$$<HTMLButtonElement>('[data-toggle-input]').forEach((b) => {
  b.addEventListener('click', () => {
    const inp = document.getElementById(b.dataset.toggleInput!) as HTMLInputElement;
    inp.checked = !inp.checked;
    b.setAttribute('aria-pressed', String(inp.checked));
    (inp.form as HTMLFormElement).requestSubmit();
  });
});
