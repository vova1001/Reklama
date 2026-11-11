// Аватар профиля: выбрать фото (камера или галерея), обрезать квадратом, загрузить — и сразу поставить в профиль.
// Убрать — крестик (вернётся буква или фото из Google/Telegram не вернётся: адрес заменён — так честнее).
import { $, $$, api } from '../lib/client';
import { guard, toast } from '../lib/ui';
import { Uploader, type Photo } from './editor/uploader';

const root = $('[data-avatar]');
if (root) {
  const url = root.dataset.photo ?? '';
  const id = url.match(/^\/media\/([A-Za-z0-9_-]{22})\//)?.[1];
  const initial: Photo[] = id ? [{ id, thumb: url, src: url.replace('/320.', '/800.'), large: url.replace('/320.', '/1600.') }] : [];
  const current = root.querySelector('img, .ava');
  root.replaceChildren();
  let last = id ?? '';
  const up: Uploader = new Uploader(root, initial, {
    purpose: 'avatar', max: 1, aspect: 1, round: true, label: 'Фото',
    onChange: async () => {
      const next = up.value()[0] ?? '';
      if (next === last) return;
      const r = await api<{ avatarUrl: string }>('PUT', '/api/me/avatar', { mediaId: next || null });
      if (!r.ok) return void (guard(r.status) || toast(r.error.message));
      last = next;
      $$<HTMLElement>('.acc-btn .ava').forEach((a) => {
        a.replaceChildren();
        if (r.data.avatarUrl) {
          const img = new Image(32, 32);
          img.src = r.data.avatarUrl;
          img.alt = '';
          a.append(img);
        } else a.textContent = (document.querySelector('.mm-name')?.textContent ?? 'О').trim()[0]?.toUpperCase() ?? 'О';
      });
      toast(next ? 'Фото обновлено' : 'Фото убрано');
    },
  });
  // фото из Google или Telegram — показать в кнопке, пока не загрузили своё
  if (!id && current?.querySelector('img')) root.querySelector('.up-add')?.prepend(current);
}
