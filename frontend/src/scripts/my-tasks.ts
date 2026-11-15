// Мои задания: действия без перезагрузки, где можно — удалить черновик/архивное (спрашиваем), вернуть с проверки,
// убрать в архив и вернуть из него, опубликовать просроченное снова (в черновик и сразу на проверку).
import { $, api, busy } from '../lib/client';
import { confirmDialog, guard, toast } from '../lib/ui';

const root = $('[data-my]');

if (root) {
  root.addEventListener('click', async (e) => {
    const b = (e.target as HTMLElement).closest<HTMLButtonElement>('button');
    if (!b) return;
    const id = b.dataset.delete ?? b.dataset.withdraw ?? b.dataset.archive ?? b.dataset.unarchive ?? b.dataset.republish;
    if (!id) return;

    if (b.dataset.delete) {
      if (!(await confirmDialog('Удалить задание?', 'Черновик и его фото удалятся.', 'Удалить'))) return b.focus();
      busy(b, true);
      const r = await api<void>('DELETE', `/api/me/tasks/${id}`);
      if (!r.ok) {
        busy(b, false);
        if (!guard(r.status)) toast(r.error.message);
        return;
      }
      const sec = b.closest<HTMLElement>('.mysec');
      b.closest('.it')?.remove();
      const left = sec?.querySelectorAll('.it').length ?? 0;
      const sup = sec?.querySelector('sup');
      if (sup) sup.textContent = String(left);
      if (sec && !left) sec.hidden = true;
      toast('Удалено');
      return;
    }

    if (b.dataset.withdraw) {
      busy(b, true);
      const r = await api<void>('POST', `/api/me/tasks/${id}/withdraw`);
      if (!r.ok) {
        busy(b, false);
        if (!guard(r.status)) toast(r.error.message);
        return;
      }
      location.assign(`/app/tasks/${id}`);
      return;
    }

    if (b.dataset.archive) {
      if (!(await confirmDialog('Убрать в архив?', 'Задание исчезнет из ленты. Вернуть можно из секции «Архивные».', 'В архив'))) return b.focus();
      busy(b, true);
      const r = await api<void>('POST', `/api/me/tasks/${id}/archive`);
      if (!r.ok) {
        busy(b, false);
        if (!guard(r.status)) toast(r.error.message);
        return;
      }
      toast('В архиве');
      location.reload();
      return;
    }

    if (b.dataset.unarchive) {
      busy(b, true);
      const r = await api<void>('POST', `/api/me/tasks/${id}/unarchive`);
      if (!r.ok) {
        busy(b, false);
        if (!guard(r.status)) toast(r.error.message);
        return;
      }
      toast('Вернулось в черновики');
      location.reload();
      return;
    }

    if (b.dataset.republish) {
      busy(b, true);
      const w = await api<void>('POST', `/api/me/tasks/${id}/withdraw`);
      if (!w.ok) {
        busy(b, false);
        if (!guard(w.status)) toast(w.error.message);
        return;
      }
      const s = await api<void>('POST', `/api/me/tasks/${id}/submit`);
      if (!s.ok) {
        busy(b, false);
        if (!guard(s.status)) toast(s.error.message);
        if (s.status === 400) location.assign(`/app/tasks/${id}`);
        return;
      }
      toast('Отправлено на проверку');
      location.reload();
    }
  });
}
