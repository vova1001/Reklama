// Мои задания: фильтр по статусу (без перезагрузки, адрес запоминает), удалить черновик (спрашиваем — удаление
// сразу на сервере), вернуть с проверки в черновик.
import { $, $$, api, busy } from '../lib/client';
import { confirmDialog, guard, toast } from '../lib/ui';

const list = $('[data-my]');
const filt = $('[data-filter]');

function applyFilter(f: string) {
  $$<HTMLButtonElement>('button', filt!).forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.f === f)));
  $$('.it', list!).forEach((it) => (it.hidden = !!f && it.dataset.status !== f));
  history.replaceState(null, '', f ? `?status=${f}` : location.pathname);
}

if (list && filt) {
  applyFilter(new URLSearchParams(location.search).get('status') ?? '');
  filt.addEventListener('click', (e) => {
    const b = (e.target as HTMLElement).closest<HTMLButtonElement>('button[data-f]');
    if (b) applyFilter(b.dataset.f!);
  });
  list.addEventListener('click', async (e) => {
    const del = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-delete]');
    const wd = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-withdraw]');
    if (del) {
      if (!(await confirmDialog('Удалить задание?', 'Черновик и его фото удалятся.', 'Удалить'))) return del.focus();
      busy(del, true);
      const r = await api<void>('DELETE', `/api/me/tasks/${del.dataset.delete}`);
      if (!r.ok) {
        busy(del, false);
        if (!guard(r.status)) toast(r.error.message);
        return;
      }
      del.closest('.it')?.remove();
      toast('Удалено');
    } else if (wd) {
      busy(wd, true);
      const r = await api<void>('POST', `/api/me/tasks/${wd.dataset.withdraw}/withdraw`);
      if (!r.ok) {
        busy(wd, false);
        if (!guard(r.status)) toast(r.error.message);
        return;
      }
      location.assign(`/app/tasks/${wd.dataset.withdraw}`);
    }
  });
}
