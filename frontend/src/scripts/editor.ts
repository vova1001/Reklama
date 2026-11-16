// Редактор задания (бизнес) и анкеты автора: черновик формы сохраняется при каждой правке — сразу на устройство,
// через ~800 мс на сервер (scripts/editor/drafts.ts); при открытии восстанавливается самое свежее (поля, блоки, порядок,
// выбранное, фото). Задание/анкета на сервере сохраняются тогда же, если поля уже проходят проверку. Проверка удобная: ошибка у поля — сразу после ухода с него; «Опубликовать»
// нажимается всегда — если что-то не заполнено, прокручиваем к первому и подсвечиваем; у кнопки — «Осталось: …»
// (каждое слово ведёт к полю). Тексты ошибок — те же, что у сервера (content/app.ts). Предпросмотр — как увидят.
import type { components } from '../lib/api.gen';
import { $, $$, api, busy, clearErrs, setErr } from '../lib/client';
import { confirmDialog, fmtNum, guard, parseNum, toast } from '../lib/ui';
import { CARD_RULES, DUE_RANGE, GOALS, PLATFORM_TITLE, TASK_RULES, type BlockKind } from '../content/app';
import { CityCombobox } from './editor/city';
import { SelectCombobox } from './editor/select';
import { formatsTitle, goal, until } from '../lib/format';
import { moneyKopecks, rewardExample, rewardTerms } from '../lib/reward';
import { BlocksEditor, type Block } from './editor/blocks';
import { CategoryPicker } from './editor/category';
import { FormatPicker } from './editor/formats';
import { DuePicker, isoDay } from './editor/due';
import { h, ic } from './editor/dom';
import { Uploader, type Photo } from './editor/uploader';
import { DraftSync } from './editor/drafts';
import { SocialList, followers, kindTitle } from './editor/social';
import { VideoList, type EdVideo } from './editor/video';

type Task = components['schemas']['Task'];
type Card = components['schemas']['Card'];
type Item = components['schemas']['CategoryItem'];
type ApiError = components['schemas']['Error']['error'];

interface GoalState { goal: string; target: number; rewardKopecks: number; paymentMode: 'fixed' | 'proportional'; link: string }
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
  goals?: GoalState[];
  bizType?: string;
  videos?: EdVideo[];
  categories: { common: Item[]; own: Item[] };
  brand: string;
  uid: number;
  updatedAt: string | null;
}

/** Состояние формы для черновика: как есть, включая то, что ещё не проходит проверку. */
interface Snap {
  v: 2;
  fields: Record<string, string>;
  nationwide: boolean;
  formats: string[];
  access: string;
  goals?: string[];
  paymentModes?: Record<string, 'fixed' | 'proportional'>;
  bizType?: string;
  selected: string[];
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
    const el = $<HTMLInputElement>(`[data-pane="edit"] input[name="${CSS.escape(k)}"], [data-pane="edit"] textarea[name="${CSS.escape(k)}"]`, form);
    if (el && typeof v === 'string') el.value = v.slice(0, el.maxLength > 0 ? el.maxLength : 1000);
  }
  const gender = d.fields?.gender;
  const genderButton = $('[data-select="gender"]', form);
  if (gender && genderButton) {
    genderButton.dataset.value = gender;
    const label = $('.select-value', genderButton);
    if (label) label.textContent = gender === 'female' ? 'Преимущественно женская' : gender === 'male' ? 'Преимущественно мужская' : 'Любая';
  }
  const rights = d.fields?.usageRights;
  const rightsButton = $('[data-select="usageRights"]', form);
  if (rights && rightsButton) {
    rightsButton.dataset.value = rights;
    const label = $('.select-value', rightsButton);
    if (label) label.textContent = rights === 'ads' ? 'Органика, репосты и реклама' : rights === 'reposts' ? 'Органика и репосты' : 'Только органика';
  }
  const nation = $('[data-nationwide]', form);
  if (nation && typeof d.nationwide === 'boolean') nation.setAttribute('aria-checked', String(d.nationwide));
  if (d.formats) init.formats = strs(d.formats).map((f) => f.slice(0, 30)).slice(0, 10);
  if (typeof d.access === 'string' && d.access) {
    $$<HTMLButtonElement>('[data-access] [role="radio"]', form).forEach((b) => {
      b.setAttribute('aria-checked', String(b.dataset.v === d.access));
      b.tabIndex = b.dataset.v === d.access ? 0 : -1;
    });
  }
  if (d.goals) {
    const chosen = new Set(strs(d.goals));
    $$<HTMLButtonElement>('[data-goal-check]', form).forEach((b) => b.setAttribute('aria-pressed', String(chosen.has(b.value))));
  }
  if (d.paymentModes) {
    for (const [id, mode] of Object.entries(d.paymentModes)) {
      $$(`[data-payment-mode="${CSS.escape(id)}"] [role="radio"]`, form).forEach((b) => b.setAttribute('aria-checked', String(b.getAttribute('data-v') === mode)));
    }
  }
  if (typeof d.bizType === 'string') {
    $$<HTMLButtonElement>('[data-biz] [role="radio"]', form).forEach((b) => b.setAttribute('aria-checked', String(b.dataset.v === d.bizType)));
  }
  if (d.selected) init.selected = strs(d.selected).filter((x) => known.has(x));
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
  // задание — одно поле описания (в payload — один текстовый блок); анкета — блочный редактор
  const blocksEl = $('[data-blocks]', form);
  const descEl = $<HTMLTextAreaElement>('[name="description"]', form);
  const blocks = blocksEl
    ? new BlocksEditor(blocksEl, init.blocks, init.templates, changed)
    : {
        value: (): Block[] => {
          const items = (descEl?.value ?? '')
            .split(/\n+/)
            .map((s) => s.trim())
            .filter(Boolean)
            .slice(0, 15)
            .map((s) => s.slice(0, 1000));
          return items.length ? [{ kind: 'text' as const, title: 'Описание', items }] : [];
        },
        showErrors: (_f: Record<string, string>) => {},
        first: (): HTMLElement | null => descEl,
      };
  const picker = new CategoryPicker($('[data-cats]', form)!, init.categories.common, init.categories.own, init.selected, {
    multi: true, max: 5, maxCustom: 3, label: isTask ? 'Сфера' : 'Темы', addLabel: 'Своя', onChange: changed,
  });
  let syncRecommendation = (_applied = false) => {};
  const renderPublications = () => {
    const box = $('[data-publications]', form);
    if (!box || !fmts) return;
    const old = new Map($$<HTMLInputElement>('[data-publication]', box).map((i) => [i.dataset.publication!, i.value]));
    box.replaceChildren(...fmts.value().map((id) => {
      const row = h('div', { class: 'publication-row' });
      const label = h('label', { class: 'lbl', for: `pub-${id}` }, `${formatsTitle([id])}, публикаций`);
      const input = h('input', { class: 'inp', id: `pub-${id}`, name: `publication.${id}`, type: 'number', min: '1', max: '100', value: old.get(id) ?? '1', 'data-publication': id });
      row.append(label, input);
      return row;
    }));
  };
  const fmts = isTask ? new FormatPicker($('[data-formats]', form)!, init.formats ?? [], { max: 10, custom: false, onChange: () => { renderPublications(); syncRecommendation(); changed(); } }) : null;
  renderPublications();
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
  const videosEl = $('[data-videos]', form);
  const videos = videosEl ? new VideoList(videosEl, init.videos ?? [], 2, changed, onBusy, { purpose: 'task_video', links: false }) : null;
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
  const bizVal = () => $<HTMLButtonElement>('[data-biz] [aria-checked="true"]', form)?.dataset.v ?? '';
  const bizBtns = $$<HTMLButtonElement>('[data-biz] [role="radio"]', form);
  const goalChecks = $$<HTMLButtonElement>('[data-goal-check]', form);
  const selectedGoals = () => goalChecks.filter((b) => b.getAttribute('aria-pressed') === 'true' && !b.closest<HTMLElement>('[data-goal-item]')!.hidden).map((b) => b.value);
  const modeVal = (id: string) => $<HTMLButtonElement>(`[data-payment-mode="${id}"] [aria-checked="true"]`, form)?.dataset.v === 'proportional' ? 'proportional' as const : 'fixed' as const;
  const goalValues = (): GoalState[] => selectedGoals().map((id) => ({
    goal: id, target: parseNum(val(`target.${id}`)), rewardKopecks: parseNum(val(`reward.${id}`)) * 100, paymentMode: modeVal(id), link: val(`link.${id}`),
  }));
  const applyPayment = () => {
    for (const id of selectedGoals().filter((x) => x !== 'barter')) {
      const proportional = modeVal(id) === 'proportional';
      const label = $(`[data-goal-item="${id}"] [data-reward-label]`, form);
      if (label) label.textContent = 'Бюджет';
      const g = goalValues().find((x) => x.goal === id)!;
      const complete = g.target > 0 && g.rewardKopecks > 0;
      const calc = $(`[data-reward-calc="${id}"]`, form);
      if (calc) calc.textContent = proportional ? rewardExample(g) : complete ? `${moneyKopecks(g.rewardKopecks)} при достижении цели` : 'Заполните цель и бюджет.';
      const summary = $(`[data-reward-summary="${id}"]`, form);
      if (summary) summary.textContent = complete ? proportional ? `Бюджет до ${moneyKopecks(g.rewardKopecks)}` : '' : '';
    }
    const total = goalValues().reduce((sum, g) => sum + g.rewardKopecks, 0);
    const out = $('[data-total-budget]', form);
    if (out) {
      out.hidden = !total;
      out.textContent = total ? `Общий бюджет: ${moneyKopecks(total)}` : '';
    }
  };
  const applyGoals = () => {
    goalChecks.forEach((b) => {
      const item = b.closest<HTMLElement>('[data-goal-item]')!;
      const terms = $('[data-goal-terms]', item);
      if (terms) terms.hidden = b.getAttribute('aria-pressed') !== 'true';
    });
    const barter = $('[data-barter-box]', form);
    if (barter) barter.hidden = !selectedGoals().includes('barter');
    applyPayment();
  };
  if (bizBtns.length && goalChecks.length) {
    const recs: Record<string, { text: string; goals: string[]; formats: string[] }> = {
      offline: { text: 'Начните с гостей, Reels и VK Клипов.', goals: ['guests'], formats: ['reels', 'vk_clips'] },
      online: { text: 'Начните с переходов, Shorts и YouTube.', goals: ['clicks'], formats: ['shorts', 'youtube'] },
      services: { text: 'Начните с подписчиков, Telegram и YouTube.', goals: ['subscribers'], formats: ['telegram', 'youtube'] },
    };
    syncRecommendation = (applied = false) => {
      const rec = recs[bizVal()];
      const button = $<HTMLButtonElement>('[data-apply-rec]', form);
      const status = $('[data-rec-status]', form);
      if (!rec || !button) return;
      const missing = rec.goals.some((id) => !selectedGoals().includes(id)) || rec.formats.some((id) => !(fmts?.value() ?? []).includes(id));
      button.disabled = !missing;
      button.textContent = missing ? 'Применить' : 'Применено';
      if (status) status.textContent = applied ? 'Рекомендации применены' : missing ? '' : 'Уже выбрано';
    };
    const applyBiz = () => {
      const biz = bizVal();
      goalChecks.forEach((b) => {
        const item = b.closest<HTMLElement>('[data-goal-item]')!;
        item.hidden = !biz || !(item.dataset.kinds ?? '').split(' ').includes(biz);
        if (item.hidden) b.setAttribute('aria-pressed', 'false');
      });
      const offline = $('[data-offline-fields]', form);
      if (offline) offline.hidden = biz !== 'offline';
      const online = $('[data-online-fields]', form);
      if (online) online.hidden = biz !== 'online';
      const box = $('[data-recommendations]', form);
      if (box) box.hidden = !biz;
      const text = $('[data-rec-text]', form);
      if (text && biz) text.textContent = recs[biz].text;
      applyGoals();
      syncRecommendation();
    };
    bizBtns.forEach((b) => b.addEventListener('click', () => {
      bizBtns.forEach((x) => x.setAttribute('aria-checked', String(x === b)));
      applyBiz();
      changed();
    }));
    goalChecks.forEach((b) => b.addEventListener('click', () => {
      b.setAttribute('aria-pressed', String(b.getAttribute('aria-pressed') !== 'true'));
      applyGoals();
      syncRecommendation();
      changed();
    }));
    $('[data-apply-rec]', form)?.addEventListener('click', () => {
      const rec = recs[bizVal()];
      if (!rec) return;
      goalChecks.filter((b) => rec.goals.includes(b.value)).forEach((b) => b.setAttribute('aria-pressed', 'true'));
      rec.formats.forEach((id) => fmts?.select(id));
      applyGoals();
      syncRecommendation(true);
      changed();
    });
    $$<HTMLButtonElement>('[data-payment-mode] [role="radio"]', form).forEach((b) => b.addEventListener('click', () => {
      const group = b.closest('[data-payment-mode]')!;
      $$<HTMLButtonElement>('[role="radio"]', group).forEach((x) => x.setAttribute('aria-checked', String(x === b)));
      applyPayment();
      changed();
    }));
    applyBiz();
  }
  const nation = $<HTMLButtonElement>('[data-nationwide]', form);
  const city = $<HTMLInputElement>('[name="city"]', form)!;
  const syncNation = () => {
    const on = nation?.getAttribute('aria-checked') === 'true';
    city.disabled = on;
    city.hidden = on;
    city.placeholder = on ? 'Вся Россия' : city.dataset.ph ?? '';
  };
  nation?.addEventListener('click', () => {
    nation.setAttribute('aria-checked', String(nation.getAttribute('aria-checked') !== 'true'));
    syncNation();
    changed();
  });
  syncNation();

  const cityList = $('[data-city-list]', form);
  if (city && cityList) new CityCombobox(city, cityList, changed);
  const genderSelect = $('[data-select="gender"]', form);
  const genderList = $('[data-select-list]', form);
  if (genderSelect && genderList) new SelectCombobox(genderSelect, genderList, [
    { value: 'any', title: 'Любая' },
    { value: 'female', title: 'Преимущественно женская' },
    { value: 'male', title: 'Преимущественно мужская' },
  ], changed);
  const rightsSelect = $('[data-select="usageRights"]', form);
  const rightsList = $('[data-rights-list]', form);
  const syncUsageDays = () => {
    const box = $('[data-usage-days]', form);
    if (box) box.hidden = rightsSelect?.dataset.value !== 'ads';
  };
  if (rightsSelect && rightsList) new SelectCombobox(rightsSelect, rightsList, [
    { value: 'organic', title: 'Только органика' },
    { value: 'reposts', title: 'Органика и репосты' },
    { value: 'ads', title: 'Органика, репосты и реклама' },
  ], () => { syncUsageDays(); changed(); });
  syncUsageDays();

  // срок — пилюли и «+ Свой» с календарём и временем ЧЧ:ММ (scripts/editor/due.ts)
  const dueTime = $<HTMLInputElement>('[name="publishTime"]', form);
  const timeOk = () => !dueTime?.value || /^([01]\d|2[0-3]):[0-5]\d$/.test(dueTime.value);
  const isoDue = () => duePick?.iso() ?? '';

  // соцсети автора — только подтверждённые площадкой (scripts/editor/social.ts); в форму анкеты не входят
  const socialEl = $('[data-social]', form);
  const social = socialEl ? new SocialList(socialEl, () => check(), () => save()) : null;

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
    if (t.matches('.inp, [name]') && !t.closest('[data-blocks], .cb')) {
      if (t.closest('[data-goal-item]')) applyPayment();
      changed();
    }
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
          goal: goalValues()[0]?.goal ?? '', link: goalValues()[0]?.link ?? '', minViews: goalValues()[0]?.target ?? 0, rewardKopecks: goalValues()[0]?.rewardKopecks ?? 0,
          goals: goalValues(), bizType: bizVal(),
          audience: { ageFrom: Number(val('ageFrom')) || 0, ageTo: Number(val('ageTo')) || 0, gender: val('gender') || 'any', language: val('language') },
          minFollowers: parseNum(val('minFollowers')), requirements: val('requirements'), script: val('script'), barter: val('barter'), responseDays: Number(val('responseDays')) || 0,
          address: bizVal() === 'offline' ? val('address') : '', delivery: bizVal() === 'online' ? val('delivery') : '',
          publications: (fmts?.value() ?? []).map((format) => ({ format, count: Number(val(`publication.${format}`)) || 1 })),
          usageRights: val('usageRights') || 'organic', usageDays: val('usageRights') === 'ads' ? Number(val('usageDays')) || 90 : 0,
          publishBy: isoDue(), publishTime: timeOk() ? val('publishTime') : '', blocks: blocks.value(),
          cover: cover.value()[0] ?? '', photos: photos.value(),
          videos: (videos?.value() ?? []).flatMap((v) => (v.media ? [v.media] : [])),
        }
      : {
          name: val('name'), city: val('city'), topics: picker.value(),
          blocks: blocks.value(), avatar: cover.value()[0] ?? '', works: photos.value(),
        };

  /** Состояние формы целиком — для черновика. */
  function snapshot(): Snap {
    const fields: Record<string, string> = {};
    $$<HTMLInputElement>('[data-pane="edit"] input[name], [data-pane="edit"] textarea[name]', form).forEach((i) => (fields[i.name] = i.value));
    return {
      v: 2, fields, nationwide: nation?.getAttribute('aria-checked') === 'true',
      formats: fmts?.value() ?? [],
      access: $<HTMLButtonElement>('[data-access] [aria-checked="true"]', form)?.dataset.v ?? '',
      goals: selectedGoals(), paymentModes: Object.fromEntries(selectedGoals().map((id) => [id, modeVal(id)])), bizType: bizVal(),
      selected: picker.value(), blocks: blocks.value(), cover: cover.photos(), photos: photos.photos(),
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
      if (bizBtns.length && !bizVal()) out.set('biz', r('biz'));
      if (title.length < 5) out.set('title', r('title'));
      if (!(p.categories as string[]).length) out.set('categories', r('categories'));
      if (!p.nationwide && (p.city as string).length < 2) out.set('city', r('city'));
      if (!(p.formats as string[]).length) out.set('formats', r('formats'));
      const gs = p.goals as GoalState[];
      if (!gs.length) out.set('goals', 'Выберите хотя бы одну цель');
      for (const g of gs) {
        if (g.goal === 'barter') {
          if (!(p.barter as string)) out.set('barter', 'Опишите, что получит автор по бартеру');
          continue;
        }
        if (!g.target) out.set(`goal.${g.goal}`, r('minViews'));
        if (g.rewardKopecks < 10000) out.set(`reward.${g.goal}`, r('reward'));
        if (g.goal === 'clicks' && !g.link) out.set('link.clicks', 'Укажите ссылку или промокод');
      }
      const ageFrom = p.audience as { ageFrom: number; ageTo: number };
      if (ageFrom.ageTo > 0 && ageFrom.ageFrom > ageFrom.ageTo) out.set('audience', 'Возраст «от» не может быть больше возраста «до»');
      if ((p.responseDays as number) > 30) out.set('requirements', 'На ответ автора — не больше 30 дней');
      const d = p.publishBy as string;
      if (!d) out.set('publishBy', r('publishBy'));
      else if (d <= isoDay(0) || d > isoDay(90)) out.set('publishBy', DUE_RANGE);
      else if (!timeOk()) out.set('publishBy', 'Время в формате ЧЧ:ММ');
      if (!hasBlocks) out.set('blocks', r('blocks'));
    } else {
      if ((p.name as string).length < 2) out.set('name', r('name'));
      if ((p.city as string).length < 2) out.set('city', r('city'));
      if (!(p.topics as string[]).length) out.set('topics', r('topics'));
      if (social && !social.verified().length) out.set('platforms', r('platforms'));
    }
    return out;
  };

  /** Ошибки у полей: у тронутых (и у всех — после «Опубликовать»); исправили — ошибка уходит сразу. */
  function check() {
    const m = problems();
    for (const k of new Set([...Object.keys(RULES), ...m.keys()])) {
      const show = tried || touched.has(k);
      const msg = show ? m.get(k) ?? '' : '';
      setErr(form, k, msg);
      const sec = $(`[data-sec="${k}"]`, form);
      if (!msg) sec?.classList.remove('is-bad');
    }
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
        const short = RULES[k]?.short ?? (k === 'goals' ? 'цели' : k === 'barter' ? 'бартер' : k === 'audience' ? 'аудитория' : 'условия цели');
        const b = h('button', { type: 'button', 'data-go': k }, short);
        b.addEventListener('click', () => goTo(k));
        leftEl.append(b);
      });
      if (keys.length > 3) leftEl.append(` и ещё ${keys.length - 3}`);
    }
  }

  // ===== шаги мастера (только задание: у формы есть [data-step]) =====
  const steps = $$<HTMLElement>('[data-step]', form);
  const wzItems = $$<HTMLElement>('[data-wz]', form);
  const stepFor = (k: string) => {
    if (k === 'biz') return 0;
    if (k === 'goals' || k === 'barter' || /^(goal|reward|link)\./.test(k)) return 1;
    if (['categories', 'city', 'formats'].includes(k)) return 2;
    if (['audience', 'requirements', 'publishBy'].includes(k)) return 3;
    if (['title', 'blocks', 'cover', 'photos', 'script'].includes(k)) return 4;
    return 5;
  };
  let step = 0;
  const showStep = (i: number) => {
    if (!steps.length) return;
    step = Math.max(0, Math.min(i, steps.length - 1));
    steps.forEach((s, j) => (s.hidden = j !== step));
    wzItems.forEach((w, j) => {
      w.classList.toggle('is-done', j < step);
      if (j === step) w.setAttribute('aria-current', 'step');
      else w.removeAttribute('aria-current');
    });
    $$<HTMLButtonElement>('[data-step-nav]', form).forEach((b) => (b.disabled = Number(b.dataset.stepNav) > step));
    const count = $('[data-step-count]', form);
    if (count) count.textContent = `Шаг ${step + 1} из ${steps.length}`;
    const prev = $<HTMLButtonElement>('[data-prev]', form);
    if (prev) prev.hidden = step === 0;
    const nextT = $('[data-next-t]', form);
    if (nextT) nextT.textContent = step === steps.length - 1 ? 'Отправить на проверку' : 'Далее';
    if (steps[step]?.dataset.step === 'review') renderReview();
  };
  $<HTMLButtonElement>('[data-prev]', form)?.addEventListener('click', () => {
    showStep(step - 1);
    steps[step]?.scrollIntoView({ block: 'start' });
  });
  $<HTMLButtonElement>('[data-next]', form)?.addEventListener('click', () => {
    const p = problems();
    const first = [...p.keys()].find((k) => stepFor(k) === step);
    if (first) {
      tried = true;
      check();
      goTo(first);
      return;
    }
    if (step < steps.length - 1) {
      showStep(step + 1);
      steps[step]?.scrollIntoView({ block: 'start' });
    } else {
      submit.click();
    }
  });
  $$<HTMLButtonElement>('[data-step-nav]', form).forEach((b) => b.addEventListener('click', () => showStep(Number(b.dataset.stepNav))));
  $<HTMLButtonElement>('[data-save]', form)?.addEventListener('click', async () => {
    dirty = true;
    await sync.flush();
    if (await save()) toast('Черновик сохранён');
  });
  showStep(0);

  /** К полю: прокрутить к середине экрана, подсветить, поставить курсор. */
  function goTo(k: string) {
    const sec = $(`[data-sec="${k}"]`, form) ?? (k === 'biz' ? $('[data-step="biz"]', form) : null);
    if (!sec) return;
    if ($('[data-pane="edit"]', form)!.hidden) $<HTMLButtonElement>('[data-view="edit"]', form)!.click();
    if (steps.length) showStep(stepFor(k));
    touched.add(k);
    check();
    const target =
      k === 'blocks'
        ? blocks.first()
        : k === 'publishBy' && duePick
          ? duePick.focusTarget()
        : k === 'platforms'
          ? social?.focusTarget()
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
    if (cover.busy() || photos.busy() || videos?.busy()) e.preventDefault();
  });

  // ===== опубликовать =====
  submit.addEventListener('click', async () => {
    tried = true;
    check();
    const first = [...problems().keys()][0];
    if (first) return goTo(first);
    if (cover.busy() || photos.busy() || videos?.busy()) return toast('Дождитесь загрузки фото и видео');
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

  function renderReview() {
    const box = $('[data-review-summary]', form);
    if (!box) return;
    const t = payload() as unknown as { title: string; city: string; nationwide: boolean; formats: string[]; goals: GoalState[]; publishBy: string; blocks: Block[]; barter: string; address: string; delivery: string; publications: { format: string; count: number }[]; usageRights: string; usageDays: number };
    const row = (label: string, value: string) => h('div', { class: 'review-row' }, h('span', {}, label), h('strong', {}, value || '—'));
    const goalsEl = h('div', { class: 'review-goals' });
    t.goals.forEach((g) => {
      const title = GOALS.find((x) => x.id === g.goal)?.title ?? g.goal;
      if (g.goal === 'barter') {
        goalsEl.append(h('div', { class: 'review-goal' }, h('div', { class: 'review-goal-top' }, h('strong', {}, title)), h('small', {}, t.barter)));
        return;
      }
      const proportional = g.paymentMode === 'proportional';
      const summary = goal(g.target, g.goal);
      const detail = proportional ? `${rewardExample(g)} · Бюджет до ${moneyKopecks(g.rewardKopecks)}` : `${moneyKopecks(g.rewardKopecks)} при достижении цели`;
      goalsEl.append(h('div', { class: 'review-goal' }, h('div', { class: 'review-goal-top' }, h('strong', {}, title), h('span', {}, summary)), h('small', {}, detail)));
    });
    box.replaceChildren(
      row('Задание', t.title),
      row('География', t.nationwide ? 'Вся Россия' : t.city),
      row('Сферы', picker.titles().join(', ')),
      row('Форматы', formatsTitle(t.formats)),
      goalsEl,
      row('Лимит бюджета', moneyKopecks(t.goals.reduce((sum, g) => sum + g.rewardKopecks, 0))),
      ...(t.address ? [row('Адрес', t.address)] : []),
      ...(t.delivery ? [row('Доставка', t.delivery)] : []),
      ...(t.publications.length ? [row('Публикации', t.publications.map((p) => `${formatsTitle([p.format])}: ${p.count}`).join(' · '))] : []),
      row('Права', t.usageRights === 'ads' ? `Органика, репосты и реклама · ${t.usageDays} дней` : t.usageRights === 'reposts' ? 'Органика и репосты' : 'Только органика'),
      row('Срок', t.publishBy ? until(t.publishBy, val('publishTime')) : ''),
    );
  }

  function taskPreview() {
    const t = payload() as unknown as { title: string; city: string; nationwide: boolean; formats: string[]; goals: GoalState[]; barter: string; publishBy: string; publishTime: string; blocks: Block[]; requirements: string; script: string };
    const where = t.nationwide ? 'Вся Россия' : t.city;
    const meta = [where, picker.titles().join(', ')].filter(Boolean).join(' · ');
    const it = h('article', { class: 'it pv-it' }, h('div', { class: 'it-meta' }, h('span', {}, init.brand ? h('b', {}, init.brand) : null, init.brand && meta ? ' · ' : '', meta)), h('h2', { class: 'it-t' }, t.title || 'Название задания'));
    const c = cover.photos()[0];
    if (c) it.append(h('img', { class: 'it-cover', src: c.src, alt: '' }));
    const facts = h('div', { class: 'facts' });
    const fact = (icon: Parameters<typeof ic>[0], strong: string | null, text: string) => facts.append(h('span', { html: ic(icon, 18) }, strong ? h('b', {}, strong) : null, text));
    for (const item of t.goals) {
      if (item.goal === 'barter') fact('gift', null, `Бартер: ${t.barter}`);
      else if (item.rewardKopecks) fact('wallet', null, `${GOALS.find((g) => g.id === item.goal)?.title ?? item.goal}: ${rewardTerms(item)}`);
      if (item.link) fact('link', null, item.link);
    }
    if (t.formats.length) fact('video', null, formatsTitle(t.formats));
    if (t.publishBy) fact('calendar', null, until(t.publishBy, t.publishTime));
    const detailBlocks = [...t.blocks];
    if (t.script) detailBlocks.push({ kind: 'text', title: 'Сценарий', items: [t.script] });
    if (t.requirements) detailBlocks.push({ kind: 'text', title: 'Требования к автору', items: [t.requirements] });
    it.append(facts, bodyOf(detailBlocks, photos.photos()));
    return it;
  }

  function cardPreview() {
    const p = payload() as unknown as { name: string; city: string; blocks: Block[] };
    const a = cover.photos()[0];
    const head = h('div', { class: 'pv-who' }, a ? h('img', { class: 'pv-ava', src: a.thumb, alt: '' }) : h('span', { class: 'pv-ava' }), h('div', {}, h('p', { class: 'it-t' }, p.name || 'Ваше имя'), h('p', { class: 'it-meta' }, p.city)));
    const tops = h('div', { class: 'chips pv-tags' });
    picker.titles().forEach((x) => tops.append(h('span', { class: 'chip is-static' }, x)));
    const pl = h('ul', { class: 'rows' });
    (social?.verified() ?? []).forEach((x) => {
      pl.append(h('li', { html: ic('badge', 20) }, h('div', {}, h('p', { class: 'row-t' }, `${PLATFORM_TITLE[x.platform] ?? x.platform} · ${x.title || x.handle}`),
        h('p', { class: 'row-s' }, `${kindTitle(x.kind)} · подтверждено`)), h('span', { class: 'row-v' }, followers(x))));
    });
    return h('article', { class: 'pv-card' }, head, tops, pl, bodyOf(p.blocks, photos.photos()));
  }

  check();
  ready = true;
  form.dataset.ready = '1';
  if (restored) changed(); // восстановленное — сохранить и в задание/анкету (если проходит проверку)
  if (init.id === null && isTask && !restored) bizBtns[0]?.focus({ preventScroll: true });
}
