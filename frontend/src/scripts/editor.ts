// Редактор задания (бизнес) и анкеты автора: черновик формы сохраняется при каждой правке — сразу на устройство,
// через ~800 мс на сервер (scripts/editor/drafts.ts); при открытии восстанавливается самое свежее (поля, блоки, порядок,
// выбранное, фото). Задание/анкета на сервере сохраняются тогда же, если поля уже проходят проверку. Проверка удобная: ошибка у поля — сразу после ухода с него; «Опубликовать»
// нажимается всегда — если что-то не заполнено, прокручиваем к первому и подсвечиваем; у кнопки — «Осталось: …»
// (каждое слово ведёт к полю). Тексты ошибок — те же, что у сервера (content/app.ts). Предпросмотр — как увидят.
import type { components } from '../lib/api.gen';
import { $, $$, api, busy, clearErrs, setErr } from '../lib/client';
import { confirmDialog, fmtNum, guard, parseNum, toast } from '../lib/ui';
import { CARD_RULES, DUE_RANGE, FOLLOWERS_MSG, PLATFORM_TITLE, PLATFORM_URL_MSG, TASK_RULES, type BlockKind } from '../content/app';
import { formatsTitle, goal, num, rub, until } from '../lib/format';
import { BlocksEditor, type Block } from './editor/blocks';
import { CategoryPicker } from './editor/category';
import { FormatPicker } from './editor/formats';
import { DuePicker, isoDay } from './editor/due';
import { appear, h, ic, vanish } from './editor/dom';
import { Uploader, type Photo } from './editor/uploader';
import { DraftSync } from './editor/drafts';

type Task = components['schemas']['Task'];
type Card = components['schemas']['Card'];
type Item = components['schemas']['CategoryItem'];
type ApiError = components['schemas']['Error']['error'];

interface Init {
  kind: 'task' | 'card';
  id: number | null;
  status: string;
  blocks: Block[];
  templates: { title: string; kind: BlockKind }[];
  cover: Photo[];
  photos: Photo[];
  selected: string[];
  formats?: string[];
  categories: { common: Item[]; own: Item[] };
  platforms: { url: string; followers: number }[];
  brand: string;
  uid: number;
  updatedAt: string | null;
}

/** Состояние формы для черновика: как есть, включая то, что ещё не проходит проверку. */
interface Snap {
  v: 1;
  fields: Record<string, string>;
  nationwide: boolean;
  formats: string[];
  access: string;
  selected: string[];
  platforms: { url: string; followers: number }[];
  blocks: Block[];
  cover: Photo[];
  photos: Photo[];
}

const form = $<HTMLFormElement>('[data-editor]');
if (form) void start(form);

const strs = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []);
const isPhoto = (p: unknown): p is Photo =>
  !!p && typeof p === 'object' && ['id', 'thumb', 'src', 'large'].every((k) => typeof (p as Record<string, unknown>)[k] === 'string') &&
  ['thumb', 'src', 'large'].every((k) => (p as Record<string, string>)[k].startsWith('/media/'));

/** Восстановить состояние формы из черновика: поля — в DOM, остальное — в начальные данные частей формы. */
function restore(form: HTMLFormElement, init: Init, d: Partial<Snap>) {
  const known = new Set([...init.categories.common, ...init.categories.own].map((c) => c.slug));
  for (const [k, v] of Object.entries(d.fields ?? {})) {
    const el = $<HTMLInputElement>(`[data-pane="edit"] input[name="${CSS.escape(k)}"]`, form);
    if (el && typeof v === 'string') el.value = v.slice(0, el.maxLength > 0 ? el.maxLength : 1000);
  }
  const nation = $('[data-nationwide]', form);
  if (nation && typeof d.nationwide === 'boolean') nation.setAttribute('aria-checked', String(d.nationwide));
  if (d.formats) init.formats = strs(d.formats).map((f) => f.slice(0, 30)).slice(0, 3);
  if (typeof d.access === 'string' && d.access) {
    $$<HTMLButtonElement>('[data-access] [role="radio"]', form).forEach((b) => {
      b.setAttribute('aria-checked', String(b.dataset.v === d.access));
      b.tabIndex = b.dataset.v === d.access ? 0 : -1;
    });
  }
  if (d.selected) init.selected = strs(d.selected).filter((x) => known.has(x));
  if (Array.isArray(d.platforms)) {
    init.platforms = d.platforms
      .filter((p) => p && typeof p.url === 'string' && typeof p.followers === 'number')
      .slice(0, 6)
      .map((p) => ({ url: p.url.slice(0, 300), followers: Math.max(0, Math.floor(p.followers)) }));
  }
  if (Array.isArray(d.blocks)) {
    init.blocks = d.blocks
      .filter((b) => b && ['text', 'list', 'links'].includes(b.kind) && typeof b.title === 'string')
      .slice(0, 8)
      .map((b) => ({ kind: b.kind, title: b.title.slice(0, 60), items: strs(b.items).slice(0, 15) }));
  }
  if (Array.isArray(d.cover)) init.cover = d.cover.filter(isPhoto).slice(0, 1);
  if (Array.isArray(d.photos)) init.photos = d.photos.filter(isPhoto).slice(0, 9);
}

async function start(form: HTMLFormElement) {
  const init = JSON.parse($('[data-init]', form)!.textContent!) as Init;
  const isTask = init.kind === 'task';
  let id = init.id;
  const stateEl0 = $('[data-state]', form)!;
  const sync = new DraftSync<Snap>(init.uid, isTask ? (id === null ? 'task-new' : `task-${id}`) : 'card', (_, text) => (stateEl0.textContent = text));
  // черновик формы пишется при каждой правке раньше задания/анкеты, поэтому он не старше их — восстановить
  // (обновили страницу до сохранения, пропала связь, поля ещё не проходят проверку); после отправки он удаляется
  form.inert = true; // пока восстанавливаем — ввод не потеряется (обычно доли секунды)
  const draft = await sync.load().finally(() => (form.inert = false));
  const restored = !!draft;
  if (restored) restore(form, init, draft!.data);
  if (draft) stateEl0.textContent = 'Сохранено · только что';
  let dirty = false;
  let saving: Promise<boolean> | null = null;
  let again = false;
  let timer = 0;
  const stateEl = $('[data-state]', form)!;
  const submit = $<HTMLButtonElement>('[data-submit]', form)!;

  const leftEl = $('[data-left]', form)!;
  const RULES = isTask ? TASK_RULES : CARD_RULES;
  const touched = new Set<string>(); // поля, с которых ушли (или все — после «Опубликовать»)
  let tried = false;
  let ready = false;

  const changed = () => {
    if (!ready) return;
    dirty = true;
    sync.touch(snapshot());
    clearTimeout(timer);
    timer = window.setTimeout(() => void save(), 800);
    check();
  };

  // ===== части формы =====
  const blocks = new BlocksEditor($('[data-blocks]', form)!, init.blocks, init.templates, changed);
  const picker = new CategoryPicker($('[data-cats]', form)!, init.categories.common, init.categories.own, init.selected, {
    multi: true, max: 5, maxCustom: 3, label: isTask ? 'Сфера' : 'Темы', addLabel: 'Своя', onChange: changed,
  });
  const fmts = isTask ? new FormatPicker($('[data-formats]', form)!, init.formats ?? [], { max: 3, onChange: changed }) : null;
  const dueBox = $('[data-due]', form);
  const duePick = dueBox ? new DuePicker(dueBox, changed) : null;
  const onBusy = (b: boolean) => {
    if (b) stateEl.textContent = 'Загружаю фото…';
  };
  const cover = new Uploader($('[data-cover]', form)!, init.cover, {
    purpose: isTask ? 'task_cover' : 'card_avatar', max: 1, aspect: isTask ? 16 / 9 : 1, round: !isTask,
    label: isTask ? 'Обложка' : 'Фото', onChange: changed, onBusy,
  });
  const photos = new Uploader($('[data-photos]', form)!, init.photos, {
    purpose: isTask ? 'task_photo' : 'card_work', max: isTask ? 6 : 9, label: 'Добавить', onChange: changed, onBusy,
  });
  const val = (n: string) => $<HTMLInputElement>(`[name="${n}"]`, form)?.value.trim() ?? '';

  // числа — с пробелами по разрядам (курсор остаётся на месте)
  $$<HTMLInputElement>('[data-num]', form).forEach((i) => {
    if (i.value) i.value = fmtNum(parseNum(i.value));
    i.addEventListener('input', () => {
      const pos = i.selectionStart ?? i.value.length;
      const digitsBefore = i.value.slice(0, pos).replace(/\D/g, '').length;
      const n = parseNum(i.value);
      i.value = n ? fmtNum(n) : '';
      let p = 0;
      for (let seen = 0; p < i.value.length && seen < digitsBefore; p++) if (/\d/.test(i.value[p])) seen++;
      i.setSelectionRange(p, p);
    });
  });

  // сегмент (кто берёт), «Вся Россия»
  $$<HTMLButtonElement>('[data-access] [role="radio"]', form).forEach((b, i, all) => {
    const pick = (x: HTMLButtonElement) => {
      all.forEach((y) => {
        y.setAttribute('aria-checked', String(y === x));
        y.tabIndex = y === x ? 0 : -1;
      });
      changed();
    };
    b.addEventListener('click', () => pick(b));
    b.addEventListener('keydown', (e) => {
      if (!['ArrowLeft', 'ArrowRight'].includes(e.key)) return;
      e.preventDefault();
      const n = all[(i + (e.key === 'ArrowRight' ? 1 : -1) + all.length) % all.length];
      n.focus();
      pick(n);
    });
  });
  const nation = $<HTMLButtonElement>('[data-nationwide]', form);
  const city = $<HTMLInputElement>('[name="city"]', form)!;
  const syncNation = () => {
    const on = nation?.getAttribute('aria-checked') === 'true';
    city.disabled = on;
    city.placeholder = on ? 'Вся Россия' : city.dataset.ph ?? '';
  };
  nation?.addEventListener('click', () => {
    nation.setAttribute('aria-checked', String(nation.getAttribute('aria-checked') !== 'true'));
    syncNation();
    changed();
  });
  syncNation();

  // срок — пилюли и «+ Свой» с календарём и временем ЧЧ:ММ (scripts/editor/due.ts)
  const dueTime = $<HTMLInputElement>('[name="publishTime"]', form);
  const timeOk = () => !dueTime?.value || /^([01]\d|2[0-3]):[0-5]\d$/.test(dueTime.value);
  const isoDue = () => duePick?.iso() ?? '';

  // площадки автора: ссылка + подписчики, добавить и убрать
  const plats = $('[data-platforms]', form);
  const addPlat = $<HTMLButtonElement>('[data-add-platform]', form);
  const platRow = (p = { url: '', followers: 0 }) => {
    const url = h('input', { class: 'inp', type: 'url', inputmode: 'url', maxlength: '300', placeholder: 'youtube.com/@канал', 'aria-label': 'Ссылка на площадку' });
    url.value = p.url.replace(/^https:\/\//, '');
    const fol = h('input', { class: 'inp', type: 'text', inputmode: 'numeric', maxlength: '13', placeholder: 'Подписчики', 'aria-label': 'Подписчики', 'data-num': '' });
    fol.value = p.followers ? fmtNum(p.followers) : '';
    fol.addEventListener('input', () => {
      const n = parseNum(fol.value);
      fol.value = n ? fmtNum(n) : '';
    });
    const rm = h('button', { class: 'ibtn', type: 'button', 'aria-label': 'Убрать площадку', html: ic('x', 18) });
    const row = h('div', { class: 'plat' }, h('span', { class: 'plat-ic', html: ic('link', 18) }), url, fol, rm);
    const err = h('p', { class: 'ferr', 'data-err-plat': '' });
    const wrap = h('div', { class: 'plat-w' }, row, err);
    // ошибка строки — после ухода из неё: ссылка без подписчиков или ссылка не похожа на адрес
    wrap.addEventListener('focusout', (e) => {
      if (wrap.contains(e.relatedTarget as Node)) return;
      wrap.dataset.touched = '1';
      check();
    });
    rm.addEventListener('click', () => void vanish(wrap).then(() => {
      syncPlats();
      changed();
      addPlat?.focus();
    }));
    return wrap;
  };
  const syncPlats = () => {
    if (addPlat && plats) addPlat.hidden = plats.children.length >= 6;
  };
  if (plats) {
    (init.platforms.length ? init.platforms : [{ url: '', followers: 0 }]).forEach((p) => plats.append(platRow(p)));
    addPlat?.addEventListener('click', () => {
      const r = platRow();
      plats.append(r);
      appear(r);
      $<HTMLInputElement>('input', r)!.focus();
      syncPlats();
    });
    syncPlats();
  }
  const platValue = () =>
    plats ? $$<HTMLElement>('.plat', plats).map((r) => ({ url: $$<HTMLInputElement>('input', r)[0].value.trim(), followers: parseNum($$<HTMLInputElement>('input', r)[1].value) })) : [];

  // счётчик у длинного поля (название — только когда близко к пределу)
  $$<HTMLInputElement>('[data-count]', form).forEach((i) => {
    const out = $(`[data-count-for="${i.name}"]`, form)!;
    const max = Number(i.maxLength);
    const upd = () => (out.textContent = i.value.length > max * 0.8 ? `${i.value.length}/${max}` : '');
    i.addEventListener('input', upd);
    upd();
  });

  form.addEventListener('input', (e) => {
    const t = e.target as HTMLElement;
    if (t.matches('.inp, [name]') && !t.closest('[data-blocks], .cb')) changed();
  });
  // ушли с поля — показать его ошибку (поле — ближайший [data-sec])
  // Ушли нажатием мыши или пальца — ошибку показываем после отпускания: иначе текст ошибки сдвигает страницу
  // между нажатием и отпусканием, и нажатие (например, на формат) теряется.
  let pressing = false;
  document.addEventListener('pointerdown', () => (pressing = true), true);
  const release = () => {
    if (!pressing) return;
    pressing = false;
    setTimeout(check, 0);
  };
  document.addEventListener('pointerup', release, true);
  document.addEventListener('pointercancel', release, true);
  form.addEventListener('focusout', (e) => {
    const sec = (e.target as HTMLElement).closest<HTMLElement>('[data-sec]');
    if (!sec || sec.contains(e.relatedTarget as Node)) return;
    touched.add(sec.dataset.sec!);
    if (!pressing) check();
  });
  form.addEventListener('submit', (e) => e.preventDefault());

  // ===== данные =====
  const payload = () =>
    isTask
      ? {
          title: val('title'), city: nation?.getAttribute('aria-checked') === 'true' ? '' : val('city'),
          nationwide: nation?.getAttribute('aria-checked') === 'true', categories: picker.value(),
          formats: fmts?.value() ?? [],
          access: $<HTMLButtonElement>('[data-access] [aria-checked="true"]', form)?.dataset.v ?? 'open',
          minViews: parseNum(val('minViews')),
          rewardKopecks: parseNum(val('reward')) * 100, publishBy: isoDue(), publishTime: timeOk() ? val('publishTime') : '', blocks: blocks.value(),
          cover: cover.value()[0] ?? '', photos: photos.value(),
        }
      : {
          name: val('name'), city: val('city'), topics: picker.value(), platforms: platValue().filter((p) => p.url || p.followers),
          blocks: blocks.value(), avatar: cover.value()[0] ?? '', works: photos.value(),
        };

  /** Состояние формы целиком — для черновика. */
  function snapshot(): Snap {
    const fields: Record<string, string> = {};
    $$<HTMLInputElement>('[data-pane="edit"] input[name]', form).forEach((i) => (fields[i.name] = i.value));
    return {
      v: 1, fields, nationwide: nation?.getAttribute('aria-checked') === 'true',
      formats: fmts?.value() ?? [],
      access: $<HTMLButtonElement>('[data-access] [aria-checked="true"]', form)?.dataset.v ?? '',
      selected: picker.value(), platforms: platValue(), blocks: blocks.value(), cover: cover.photos(), photos: photos.photos(),
    };
  }

  /** Что ещё нужно для публикации (как на сервере): поле → текст ошибки. Порядок — как поля на экране. */
  const problems = (): Map<string, string> => {
    const p = payload() as Record<string, unknown>;
    const out = new Map<string, string>();
    const r = (k: string) => RULES[k].msg;
    const hasBlocks = (p.blocks as Block[]).some((b) => b.items.length);
    if (isTask) {
      const title = p.title as string;
      if (title.length < 5) out.set('title', r('title'));
      if (!(p.categories as string[]).length) out.set('categories', r('categories'));
      if (!p.nationwide && (p.city as string).length < 2) out.set('city', r('city'));
      if (!(p.formats as string[]).length) out.set('formats', r('formats'));
      if (!p.minViews) out.set('minViews', r('minViews'));
      if ((p.rewardKopecks as number) < 10000) out.set('reward', r('reward'));
      const d = p.publishBy as string;
      if (!d) out.set('publishBy', r('publishBy'));
      else if (d <= isoDay(0) || d > isoDay(90)) out.set('publishBy', DUE_RANGE);
      else if (!timeOk()) out.set('publishBy', 'Время в формате ЧЧ:ММ');
      if (!hasBlocks) out.set('blocks', r('blocks'));
    } else {
      if ((p.name as string).length < 2) out.set('name', r('name'));
      if ((p.city as string).length < 2) out.set('city', r('city'));
      if (!(p.topics as string[]).length) out.set('topics', r('topics'));
      if (!(p.platforms as unknown[]).length) out.set('platforms', r('platforms'));
      else if (platProblems().some(Boolean)) out.set('platforms', FOLLOWERS_MSG);
    }
    return out;
  };

  /** Ошибки строк площадок (по порядку строк): ссылка не похожа на адрес; есть ссылка, нет подписчиков. */
  const platProblems = (): string[] =>
    plats
      ? $$<HTMLElement>('.plat', plats).map((row) => {
          const [u, f] = $$<HTMLInputElement>('input', row);
          const url = u.value.trim();
          if (!url && !parseNum(f.value)) return '';
          if (!/^(https?:\/\/)?[^\s/.]+\.[^\s]{2,}$/i.test(url)) return PLATFORM_URL_MSG;
          return parseNum(f.value) ? '' : FOLLOWERS_MSG;
        })
      : [];

  /** Ошибки у полей: у тронутых (и у всех — после «Опубликовать»); исправили — ошибка уходит сразу. */
  function check() {
    const m = problems();
    for (const k of Object.keys(RULES)) {
      const show = tried || touched.has(k);
      const msg = show ? m.get(k) ?? '' : '';
      setErr(form, k, k === 'platforms' && msg === FOLLOWERS_MSG ? '' : msg);
      const sec = $(`[data-sec="${k}"]`, form);
      if (!msg) sec?.classList.remove('is-bad');
    }
    // площадки — у строк
    const pp = platProblems();
    $$<HTMLElement>('.plat-w', form).forEach((w, i) => {
      const msg = tried || w.dataset.touched ? pp[i] ?? '' : '';
      $('[data-err-plat]', w)!.textContent = msg;
      $$('input', w).forEach((x) => x.setAttribute('aria-invalid', String(!!msg)));
    });
    // «Осталось: …» у кнопки (перестраивается, только если список изменился — иначе щелчок по слову теряется)
    const keys = [...m.keys()];
    const sig = keys.map((k) => `${k}:${m.get(k)}`).join('|');
    if (sig === leftEl.dataset.sig) return;
    leftEl.dataset.sig = sig;
    leftEl.replaceChildren();
    leftEl.hidden = !keys.length;
    if (keys.length) {
      leftEl.append('Осталось: ');
      keys.slice(0, 3).forEach((k, i) => {
        if (i) leftEl.append(', ');
        const b = h('button', { type: 'button', 'data-go': k }, k === 'platforms' && m.get(k) === FOLLOWERS_MSG ? 'подписчики' : RULES[k].short);
        b.addEventListener('click', () => goTo(k));
        leftEl.append(b);
      });
      if (keys.length > 3) leftEl.append(` и ещё ${keys.length - 3}`);
    }
  }

  /** К полю: прокрутить к середине экрана, подсветить, поставить курсор. */
  function goTo(k: string) {
    const sec = $(`[data-sec="${k}"]`, form);
    if (!sec) return;
    if ($('[data-pane="edit"]', form)!.hidden) $<HTMLButtonElement>('[data-view="edit"]', form)!.click();
    touched.add(k);
    check();
    const target =
      k === 'blocks'
        ? blocks.first()
        : k === 'publishBy' && duePick
          ? duePick.focusTarget()
        : k === 'platforms'
          ? ($$<HTMLElement>('.plat-w', form).find((w) => $('[data-err-plat]', w)!.textContent)?.querySelector<HTMLInputElement>('input') ??
            $$<HTMLInputElement>('.plat input', form)[0] ??
            $('[data-add-platform]', form))
          : $<HTMLElement>(`[name="${k}"]:not(:disabled), [data-input="${k}"] button, [data-input="${k}"] input`, sec);
    sec.scrollIntoView({ behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'center' });
    target?.focus({ preventScroll: true });
    sec.classList.remove('is-bad');
    void sec.offsetWidth; // перезапуск подсветки
    sec.classList.add('is-bad');
  }

  const showErrors = (e: ApiError) => {
    const f = e.fields ?? {};
    for (const [k, v] of Object.entries(f)) {
      if (k.startsWith('blocks.')) continue;
      if (k.startsWith('platforms.')) {
        const el = $$('[data-err-plat]', form)[Number(k.split('.')[1])];
        if (el) el.textContent = v;
        continue;
      }
      setErr(form, k, v);
    }
    blocks.showErrors(f);
    if (!Object.keys(f).length) toast(e.message);
  };

  // ===== сохранение =====
  async function save(): Promise<boolean> {
    clearTimeout(timer);
    if (saving) {
      again = true;
      return saving;
    }
    if (!dirty && id !== null) return true;
    dirty = false;
    const body = payload();
    saving = (async () => {
      const r = isTask
        ? await api<Task>(id === null ? 'POST' : 'PUT', id === null ? '/api/me/tasks' : `/api/me/tasks/${id}`, body)
        : await api<{ card: Card }>('PUT', '/api/me/card', body);
      if (!r.ok) {
        dirty = true;
        if (guard(r.status)) return false;
        if (r.status === 409) {
          toast(r.error.message);
          setTimeout(() => location.reload(), 1500);
          return false;
        }
        if (r.status === 0) {
          timer = window.setTimeout(() => void save(), 5000);
          return false;
        }
        clearErrs(form);
        showErrors(r.error);
        return false;
      }
      if (isTask && id === null) {
        id = (r.data as Task).id;
        history.replaceState(null, '', `/app/tasks/${id}`);
        $('[data-delete-wrap]', form)?.removeAttribute('hidden');
        void sync.rename(`task-${id}`);
      }
      clearErrs(form);
      blocks.showErrors({});
      check();
      return true;
    })();
    const ok = await saving;
    saving = null;
    if (again) {
      again = false;
      return save();
    }
    return ok;
  }

  // уходим — черновик уже на устройстве; отправить на сервер, что успеем. Предупреждаем только о незагруженных фото.
  window.addEventListener('pagehide', () => {
    void sync.flush();
    if (dirty) void save();
  });
  window.addEventListener('beforeunload', (e) => {
    if (cover.busy() || photos.busy()) e.preventDefault();
  });

  // ===== опубликовать =====
  submit.addEventListener('click', async () => {
    tried = true;
    check();
    const first = [...problems().keys()][0];
    if (first) return goTo(first);
    if (cover.busy() || photos.busy()) return toast('Дождитесь загрузки фото');
    busy(submit, true);
    const saved = await save();
    if (!saved) return busy(submit, false);
    const r = isTask ? await api<Task>('POST', `/api/me/tasks/${id}/submit`) : await api<{ card: Card }>('POST', '/api/me/card/submit');
    if (!r.ok) {
      busy(submit, false);
      if (guard(r.status)) return;
      if (r.status === 400) {
        showErrors(r.error);
        const f = Object.keys(r.error.fields ?? {})[0]?.split('.')[0];
        if (f) goTo(f);
        return;
      }
      return toast(r.error.message);
    }
    dirty = false;
    await sync.clear();
    location.assign(isTask ? `/app/tasks/${id}?sent=1` : '/app/card?sent=1');
  });

  // ✕ — сохранить, если не успели, и закрыть
  $<HTMLAnchorElement>('[data-close]', form)?.addEventListener('click', async (e) => {
    if (!dirty && !saving) return;
    e.preventDefault();
    const href = (e.currentTarget as HTMLAnchorElement).href;
    await sync.flush(); // введённое — в черновике, даже если задание ещё не проходит проверку
    await save();
    dirty = false;
    location.assign(href);
  });

  $('[data-delete]', form)?.addEventListener('click', async (e) => {
    const b = e.currentTarget as HTMLButtonElement;
    if (!(await confirmDialog('Удалить черновик?', 'Задание и его фото удалятся.', 'Удалить'))) return b.focus();
    clearTimeout(timer);
    dirty = false;
    if (id !== null) {
      const r = await api<void>('DELETE', `/api/me/tasks/${id}`);
      if (!r.ok && !guard(r.status)) return toast(r.error.message);
    }
    await sync.clear();
    location.assign('/app/tasks');
  });

  // ===== предпросмотр =====
  const tabs = $$<HTMLButtonElement>('[data-view]', form);
  tabs.forEach((t, i) => {
    const pick = (x: HTMLButtonElement) => {
      tabs.forEach((y) => {
        y.setAttribute('aria-selected', String(y === x));
        y.tabIndex = y === x ? 0 : -1;
      });
      const pv = x.dataset.view === 'preview';
      $('[data-pane="edit"]', form)!.hidden = pv;
      $('[data-pane="preview"]', form)!.hidden = !pv;
      if (pv) renderPreview();
    };
    t.addEventListener('click', () => pick(t));
    t.addEventListener('keydown', (e) => {
      if (!['ArrowLeft', 'ArrowRight'].includes(e.key)) return;
      e.preventDefault();
      const n = tabs[(i + 1) % tabs.length];
      n.focus();
      pick(n);
    });
  });

  function renderPreview() {
    const box = $('[data-preview]', form)!;
    box.replaceChildren(isTask ? taskPreview() : cardPreview());
  }

  const bodyOf = (bs: Block[], ph: Photo[]) => {
    const body = h('div', { class: 'body' });
    for (const b of bs.filter((x) => x.items.length)) {
      const sec = h('section', { class: 'body-b' }, b.title ? h('h3', {}, b.title) : null);
      if (b.kind === 'text') b.items.forEach((p) => sec.append(h('p', {}, p)));
      else {
        const ul = h('ul', { class: `k-${b.kind}` });
        b.items.forEach((i) => ul.append(h('li', {}, b.kind === 'links' ? h('a', { href: i.startsWith('http') ? i : `https://${i}`, target: '_blank', rel: 'nofollow noopener noreferrer' }, i.replace(/^https?:\/\//, '')) : i)));
        sec.append(ul);
      }
      body.append(sec);
    }
    if (ph.length) {
      const g = h('div', { class: 'body-ph' });
      ph.forEach((p) => g.append(h('img', { src: p.thumb, alt: '', loading: 'lazy' })));
      body.append(g);
    }
    return body;
  };

  function taskPreview() {
    const t = payload() as unknown as { title: string; city: string; nationwide: boolean; formats: string[]; minViews: number; rewardKopecks: number; publishBy: string; publishTime: string; blocks: Block[] };
    const where = t.nationwide ? 'Вся Россия' : t.city;
    const meta = [where, picker.titles().join(', ')].filter(Boolean).join(' · ');
    const it = h('article', { class: 'it pv-it' }, h('div', { class: 'it-meta' }, h('span', {}, init.brand ? h('b', {}, init.brand) : null, init.brand && meta ? ' · ' : '', meta)), h('h2', { class: 'it-t' }, t.title || 'Название задания'));
    const c = cover.photos()[0];
    if (c) it.append(h('img', { class: 'it-cover', src: c.src, alt: '' }));
    const facts = h('div', { class: 'facts' });
    const fact = (icon: Parameters<typeof ic>[0], strong: string | null, text: string) => facts.append(h('span', { html: ic(icon, 18) }, strong ? h('b', {}, strong) : null, text));
    if (t.rewardKopecks) fact('wallet', rub(t.rewardKopecks), '');
    const g = goal(t.minViews);
    if (g) fact('eye', null, g);
    if (t.formats.length) fact('video', null, formatsTitle(t.formats));
    if (t.publishBy) fact('calendar', null, until(t.publishBy, t.publishTime));
    it.append(facts, bodyOf(t.blocks, photos.photos()));
    return it;
  }

  function cardPreview() {
    const p = payload() as unknown as { name: string; city: string; platforms: { url: string; followers: number }[]; blocks: Block[] };
    const a = cover.photos()[0];
    const head = h('div', { class: 'pv-who' }, a ? h('img', { class: 'pv-ava', src: a.thumb, alt: '' }) : h('span', { class: 'pv-ava' }), h('div', {}, h('p', { class: 'it-t' }, p.name || 'Ваше имя'), h('p', { class: 'it-meta' }, p.city)));
    const tops = h('div', { class: 'chips pv-tags' });
    picker.titles().forEach((x) => tops.append(h('span', { class: 'chip is-static' }, x)));
    const pl = h('ul', { class: 'rows' });
    p.platforms.forEach((x) => {
      let host = x.url;
      try {
        host = new URL(x.url.includes('://') ? x.url : `https://${x.url}`).hostname.replace(/^www\./, '');
      } catch {
        /* показать как есть */
      }
      const kind = Object.entries({ telegram: /t\.me|telegram/, youtube: /youtu/, vk: /vk\.(com|ru)|vkvideo/, tiktok: /tiktok/, instagram: /instagram/, dzen: /dzen/, rutube: /rutube/ }).find(([, re]) => re.test(host))?.[0] ?? 'other';
      pl.append(h('li', { html: ic('link', 20) }, h('div', {}, h('p', { class: 'row-t' }, PLATFORM_TITLE[kind]), h('p', { class: 'row-s' }, host)), h('span', { class: 'row-v' }, x.followers ? `${num(x.followers)} подписч.` : '')));
    });
    return h('article', { class: 'pv-card' }, head, tops, pl, bodyOf(p.blocks, photos.photos()));
  }

  check();
  ready = true;
  form.dataset.ready = '1';
  if (restored) changed(); // восстановленное — сохранить и в задание/анкету (если проходит проверку)
  if (init.id === null && isTask && !restored) $<HTMLInputElement>('[name="title"]', form)?.focus({ preventScroll: true });
}
