// «Пожаловаться»: свой диалог с причинами (радиогруппа на кнопках, стрелки), комментарий, отправка; ответ — сообщением.
import { $, $$, api, busy, clearErrs, setErr, showFields } from '../lib/client';
import { guard, toast } from '../lib/ui';

$$<HTMLButtonElement>('[data-complain]').forEach((open) => {
  const dlg = document.getElementById(open.dataset.complain!) as HTMLDialogElement | null;
  if (!dlg || dlg.dataset.ready) return;
  dlg.dataset.ready = '1';
  const form = $<HTMLFormElement>('form', dlg)!;
  const radios = $$<HTMLButtonElement>('[data-reason]', dlg);
  let reason = '';
  const pick = (b: HTMLButtonElement) => {
    reason = b.dataset.reason!;
    radios.forEach((x) => {
      x.setAttribute('aria-checked', String(x === b));
      x.tabIndex = x === b ? 0 : -1;
    });
    setErr(form, 'reason', '');
  };
  radios.forEach((b, i) => {
    b.addEventListener('click', () => pick(b));
    b.addEventListener('keydown', (e) => {
      const d = e.key === 'ArrowDown' || e.key === 'ArrowRight' ? 1 : e.key === 'ArrowUp' || e.key === 'ArrowLeft' ? -1 : 0;
      if (!d) return;
      e.preventDefault();
      const n = radios[(i + d + radios.length) % radios.length];
      n.focus();
      pick(n);
    });
  });
  open.addEventListener('click', () => {
    clearErrs(form);
    dlg.showModal();
    radios[0]?.focus();
  });
  $('[data-cmp-cancel]', dlg)?.addEventListener('click', () => dlg.close());
  $<HTMLButtonElement>('[data-cmp-send]', dlg)?.addEventListener('click', async (e) => {
    const b = e.currentTarget as HTMLButtonElement;
    clearErrs(form);
    const comment = $<HTMLTextAreaElement>('textarea', dlg)!.value.trim();
    if (!reason) return setErr(form, 'reason', 'Выберите причину');
    if (reason === 'other' && comment.length < 5) return setErr(form, 'comment', 'Опишите, что не так (от 5 символов)');
    busy(b, true);
    const r = await api<{ created: boolean }>('POST', '/api/complaints', { subject: dlg.dataset.subject, id: Number(dlg.dataset.id), reason, comment });
    busy(b, false);
    if (!r.ok) {
      if (guard(r.status)) return;
      if (r.error.fields) return showFields(form, r.error);
      dlg.close();
      return toast(r.error.message);
    }
    dlg.close();
    form.reset();
    toast(r.data.created ? 'Спасибо, жалоба отправлена — проверим' : 'Вы уже пожаловались — проверим');
  });
});
