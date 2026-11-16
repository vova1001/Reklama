// Мои задания: по умолчанию видны все, фильтр показывает выбранный статус. Действия — удалить
// черновик/архивное (спрашиваем), вернуть с проверки, убрать в архив и опубликовать просроченное снова.
import { $, $$, api, busy } from '../lib/client';
import { confirmDialog, guard, toast } from '../lib/ui';

const root = $('[data-my]');
const filt = $('[data-filter]');

if (filt && root) {
  let selected = '';
  filt.addEventListener('click', (e) => {
    const b = (e.target as HTMLElement).closest<HTMLButtonElement>('button[data-f]');
    if (!b) return;
    const f = b.dataset.f!;
    selected = selected === f ? '' : f;
    $$<HTMLButtonElement>('button', filt).forEach((x) => x.setAttribute('aria-pressed', String(x.dataset.f === selected)));
    $$<HTMLElement>('[data-sec-list]', root).forEach((s) => (s.hidden = !!selected && s.dataset.secList !== selected));
  });
}

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
      sec?.querySelectorAll('sup').forEach((sup) => (sup.textContent = String(left)));
      if (filt) {
        const chip = $<HTMLButtonElement>(`[data-f="${sec?.dataset.secList}"]`, filt)?.querySelector('sup');
        if (chip) chip.textContent = String(left);
        const all = filt.querySelector('[data-f=""] sup');
        if (all) all.textContent = String(Number(all.textContent) - 1);
      }
      if (sec && !left) {
        sec.querySelector('.list')?.remove();
        const none = document.createElement('p');
        none.className = 'mysec-none';
        none.textContent = 'Пока пусто';
        sec.append(none);
      }
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
