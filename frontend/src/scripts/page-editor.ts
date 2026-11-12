// Редактор страницы бизнеса и профиля автора — тот же порядок, что у задания: черновик формы при каждой правке —
// сразу на устройство и через ~800 мс на сервер (scripts/editor/drafts.ts), при открытии — самое свежее; страница
// сохраняется тогда же. Адрес — отдельно (PUT …/slug): занят — ошибка у поля. «Опубликовать» нажимается всегда:
// не хватает — прокрутка к полю и «Осталось: …». «Как увидят» — та же разметка, что у публичной страницы.
import type { components } from '../lib/api.gen';
import { $, $$, api, busy, clearErrs, setErr } from '../lib/client';
import { guard, toast } from '../lib/ui';
import { PAGE_ABOUT_CREATOR, PAGE_BLOCKS, PAGE_PATH, PAGE_RULES, type PageKind } from '../content/app';
import { CategoryPicker } from './editor/category';
import { DraftSync } from './editor/drafts';
import { h, ic } from './editor/dom';
import { Uploader, type Photo } from './editor/uploader';
import { PageBlocks, emptyBlock, type EdBlock } from './editor/page-blocks';
import type { EdVideo } from './editor/video';

type Page = components['schemas']['Page'];
type Item = components['schemas']['CategoryItem'];
type ApiError = components['schemas']['Error']['error'];

interface Init {
  kind: PageKind;
  exists: boolean;
  status: string;
  slug: string;
  title: string;
  city: string;
  spheres: string[];
  logo: Photo[];
  cover: Photo[];
  blocks: EdBlock[];
  categories: { common: Item[]; own: Item[] };
  uid: number;
  origin: string;
}

/** Состояние формы для черновика. */
interface Snap {
  v: 1;
  slug: string;
  title: string;
  city: string;
  spheres: string[];
  logo: Photo[];
  cover: Photo[];
  blocks: EdBlock[];
}

const form = $<HTMLFormElement>('[data-page-editor]');
if (form) void start(form);

const strs = (v: unknown, n: number, len = 3000): string[] =>
  Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string').slice(0, n).map((x) => x.slice(0, len)) : [];
const str = (v: unknown, len: number) => (typeof v === 'string' ? v.slice(0, len) : '');
const isPhoto = (p: unknown): p is Photo =>
  !!p && typeof p === 'object' && ['id', 'thumb', 'src', 'large'].every((k) => typeof (p as Record<string, unknown>)[k] === 'string') &&
  ['thumb', 'src', 'large'].every((k) => (p as Record<string, string>)[k].startsWith('/media/'));
const isVideo = (v: unknown): v is EdVideo => {
  if (!v || typeof v !== 'object') return false;
  const x = v as Record<string, unknown>;
  if (typeof x.url === 'string') return true;
  return typeof x.media === 'string' && typeof x.src === 'string' && x.src.startsWith('/media/') && (x.poster == null || isPhoto(x.poster));
};

/** Блоки из черновика: только известные виды и поля, с пределами. */
function cleanBlocks(kind: PageKind, raw: unknown): EdBlock[] | null {
  if (!Array.isArray(raw)) return null;
  const types = new Set(Object.keys(PAGE_BLOCKS));
  return raw
    .filter((b) => b && typeof b === 'object' && types.has((b as EdBlock).type))
    .slice(0, 20)
    .map((b: EdBlock) => ({
      ...emptyBlock(b.type),
      title: str(b.title, 80), text: str(b.text, 3000), items: strs(b.items, 20, 200), links: strs(b.links, 10, 500),
      photos: Array.isArray(b.photos) ? b.photos.filter(isPhoto).slice(0, 12) : [],
      videos: Array.isArray(b.videos) ? b.videos.filter(isVideo).slice(0, 3) : [],
      contacts: b.contacts && typeof b.contacts === 'object'
        ? { city: str(b.contacts.city, 80), address: str(b.contacts.address, 200), hours: str(b.contacts.hours, 120), site: str(b.contacts.site, 300), phone: str(b.contacts.phone, 30), socials: strs(b.contacts.socials, 6, 300) }
        : emptyBlock(b.type).contacts,
    }))
    .filter((b) => kind === 'creator' || !['channels', 'topics', 'works', 'reviews', 'card'].includes(b.type));
}

async function start(form: HTMLFormElement) {
  const init = JSON.parse($('[data-init]', form)!.textContent!) as Init;
  const kind = init.kind;
  const RULES = PAGE_RULES[kind];
  const stateEl = $('[data-state]', form)!;
  const sync = new DraftSync<Snap>(init.uid, `page-${kind}`, (_, text) => (stateEl.textContent = text));
  form.inert = true;
  const draft = await sync.load().finally(() => (form.inert = false));
  if (draft) {
    const d = draft.data;
    init.slug = init.exists ? init.slug : str(d.slug, 40) || init.slug;
    init.title = str(d.title, 80);
    init.city = str(d.city, 80);
    init.spheres = strs(d.spheres, 5, 40);
    if (Array.isArray(d.logo)) init.logo = d.logo.filter(isPhoto).slice(0, 1);
    if (Array.isArray(d.cover)) init.cover = d.cover.filter(isPhoto).slice(0, 1);
    init.blocks = cleanBlocks(kind, d.blocks) ?? init.blocks;
    stateEl.textContent = 'Сохранено · только что';
  }
  let exists = init.exists;
  let dirty = false;
  let saving: Promise<boolean> | null = null;
  let again = false;
  let timer = 0;
  let tried = false;
  let ready = false;
  const touched = new Set<string>();
  const submit = $<HTMLButtonElement>('[data-submit]', form)!;
  const leftEl = $('[data-left]', form)!;

  const changed = () => {
    if (!ready) return;
    dirty = true;
    sync.touch(snapshot());
    clearTimeout(timer);
    timer = window.setTimeout(() => void save(), 800);
    check();
  };
  let uploads = 0;
  const onBusy = (b: boolean) => {
    uploads += b ? 1 : -1;
    if (uploads > 0) stateEl.textContent = 'Загружаю…';
  };

  // ===== поля =====
  const titleIn = $<HTMLInputElement>('[name="title"]', form)!;
  const cityIn = $<HTMLInputElement>('[name="city"]', form)!;
  const slugIn = $<HTMLInputElement>('[name="slug"]', form)!;
  titleIn.value = init.title;
  cityIn.value = init.city;
  slugIn.value = init.slug;
  const cover = new Uploader($('[data-cover]', form)!, init.cover, { purpose: 'page_cover', max: 1, aspect: 3, label: 'Обложка', onChange: changed, onBusy });
  const logo = new Uploader($('[data-logo]', form)!, init.logo, { purpose: 'page_logo', max: 1, aspect: 1, round: kind === 'creator', label: kind === 'creator' ? 'Фото' : 'Логотип', onChange: changed, onBusy });
  const known = new Set([...init.categories.common, ...init.categories.own].map((c) => c.slug));
  const picker = new CategoryPicker($('[data-cats]', form)!, init.categories.common, init.categories.own, init.spheres.filter((s) => known.has(s)), {
    multi: true, max: 5, maxCustom: 3, label: kind === 'creator' ? 'Темы' : 'Сферы', addLabel: 'Своя', onChange: changed,
  });
  const blocks = new PageBlocks($('[data-pblocks]', form)!, init.blocks, { kind, categories: init.categories, onChange: changed, onBusy });
  const busyNow = () => cover.busy() || logo.busy() || blocks.busy();

  // адрес: показываем ссылку целиком; новый — уйдёт с первым сохранением, у существующей — отдельным запросом
  const urlEl = $('[data-slug-url]', form)!;
  const showURL = () => (urlEl.textContent = `${init.origin}${PAGE_PATH[kind]}${slugIn.value.trim().toLowerCase() || '…'}`);
  showURL();
  let slugSaved = init.slug;
  const saveSlug = async () => {
    const v = slugIn.value.trim().toLowerCase();
    slugIn.value = v;
    showURL();
    if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(v) || v.length < 3 || v.length > 40) {
      setErr(form, 'slug', 'Адрес — от 3 до 40 символов: латиница, цифры и дефис');
      return;
    }
    setErr(form, 'slug', '');
    if (v === slugSaved) return;
    if (!exists) return changed();
    const r = await api<{ page: Page }>('PUT', `/api/me/pages/${kind}/slug`, { slug: v });
    if (!r.ok) {
      if (guard(r.status)) return;
      return setErr(form, 'slug', r.error.fields?.slug ?? r.error.message);
    }
    slugSaved = r.data.page.slug;
    toast('Адрес сохранён — прежний ведёт на новый');
  };
  slugIn.addEventListener('input', () => {
    slugIn.value = slugIn.value.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-{2,}/g, '-').slice(0, 40);
    showURL();
  });
  slugIn.addEventListener('change', () => void saveSlug());
  slugIn.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      void saveSlug();
    }
  });

  form.addEventListener('input', (e) => {
    const t = e.target as HTMLElement;
    if (t === titleIn || t === cityIn) changed();
  });
  form.addEventListener('focusout', (e) => {
    const sec = (e.target as HTMLElement).closest<HTMLElement>('[data-sec]');
    if (!sec || sec.contains(e.relatedTarget as Node)) return;
    touched.add(sec.dataset.sec!);
    check();
  });
  form.addEventListener('submit', (e) => e.preventDefault());

  // ===== данные =====
  const payload = () => ({
    slug: exists ? '' : slugIn.value.trim(),
    title: titleIn.value.trim(), city: cityIn.value.trim(), spheres: picker.value(),
    logo: logo.value()[0] ?? '', cover: cover.value()[0] ?? '',
    blocks: blocks.value().map((b) => ({
      type: b.type, title: b.title, text: b.text, items: b.items, photos: b.photos.map((p) => p.id), links: b.links,
      videos: b.videos.map((v) => (v.url ? { url: v.url } : { media: v.media ?? '', poster: v.poster?.id ?? '' })),
      contacts: b.type === 'contacts' ? b.contacts : null,
    })),
  });
  const snapshot = (): Snap => ({
    v: 1, slug: slugIn.value.trim(), title: titleIn.value, city: cityIn.value, spheres: picker.value(),
    logo: logo.photos(), cover: cover.photos(), blocks: blocks.value(),
  });

  const filled = (b: EdBlock) => {
    if (b.type === 'channels' || b.type === 'reviews' || b.type === 'card') return false;
    const c = b.contacts;
    return !!(b.text || b.items.length || b.photos.length || b.videos.length || b.links.length || (c && (c.city || c.address || c.hours || c.site || c.phone || c.socials.length)));
  };
  const problems = (): Map<string, string> => {
    const out = new Map<string, string>();
    if (titleIn.value.trim().length < 2) out.set('title', RULES.title.msg);
    if (cityIn.value.trim().length < 2) out.set('city', RULES.city.msg);
    if (!picker.value().length) out.set('spheres', RULES.spheres.msg);
    if (!blocks.value().some(filled)) out.set('blocks', RULES.blocks.msg);
    return out;
  };

  function check() {
    const m = problems();
    for (const k of Object.keys(RULES)) {
      setErr(form, k, tried || touched.has(k) ? m.get(k) ?? '' : '');
      if (!m.get(k)) $(`[data-sec="${k}"]`, form)?.classList.remove('is-bad');
    }
    const keys = [...m.keys()];
    const sig = keys.join('|');
    if (sig === leftEl.dataset.sig) return;
    leftEl.dataset.sig = sig;
    leftEl.replaceChildren();
    leftEl.hidden = !keys.length;
    if (keys.length) {
      leftEl.append('Осталось: ');
      keys.forEach((k, i) => {
        if (i) leftEl.append(', ');
        const b = h('button', { type: 'button', 'data-go': k }, RULES[k].short);
        b.addEventListener('click', () => goTo(k));
        leftEl.append(b);
      });
    }
  }

  function goTo(k: string) {
    const sec = $(`[data-sec="${k}"]`, form);
    if (!sec) return;
    if ($('[data-pane="edit"]', form)!.hidden) $<HTMLButtonElement>('[data-view="edit"]', form)!.click();
    touched.add(k);
    check();
    const target = k === 'blocks' ? blocks.first() : $<HTMLElement>(`[name="${k}"], [data-input="${k}"] button`, sec);
    sec.scrollIntoView({ behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'center' });
    target?.focus({ preventScroll: true });
    sec.classList.remove('is-bad');
    void sec.offsetWidth;
    sec.classList.add('is-bad');
  }

  const showErrors = (e: ApiError) => {
    const f = e.fields ?? {};
    for (const [k, v] of Object.entries(f)) if (!k.startsWith('blocks.')) setErr(form, k, v);
    blocks.showErrors(f);
    if (!Object.keys(f).length) toast(e.message);
  };

  async function save(): Promise<boolean> {
    clearTimeout(timer);
    if (saving) {
      again = true;
      return saving;
    }
    if (!dirty && exists) return true;
    dirty = false;
    const body = payload();
    saving = (async () => {
      const r = await api<{ page: Page }>('PUT', `/api/me/pages/${kind}`, body);
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
      if (!exists) {
        exists = true;
        slugSaved = r.data.page.slug;
        slugIn.value = slugSaved;
        showURL();
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

  window.addEventListener('pagehide', () => {
    void sync.flush();
    if (dirty) void save();
  });
  window.addEventListener('beforeunload', (e) => {
    if (busyNow()) e.preventDefault();
  });

  submit.addEventListener('click', async () => {
    tried = true;
    check();
    const first = [...problems().keys()][0];
    if (first) return goTo(first);
    if (busyNow()) return toast('Дождитесь загрузки фото и видео');
    busy(submit, true);
    if (!(await save())) return busy(submit, false);
    const r = await api<{ page: Page }>('POST', `/api/me/pages/${kind}/submit`);
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
    location.assign(`/app/page?kind=${kind}&sent=1`);
  });

  $<HTMLAnchorElement>('[data-close]', form)?.addEventListener('click', async (e) => {
    if (!dirty && !saving) return;
    e.preventDefault();
    const href = (e.currentTarget as HTMLAnchorElement).href;
    await sync.flush();
    await save();
    dirty = false;
    location.assign(href);
  });

  // ===== «Как увидят» =====
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
      if (pv) $('[data-preview]', form)!.replaceChildren(preview());
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

  function preview(): HTMLElement {
    const art = h('article', { class: `pg is-${kind}` });
    const cv = cover.photos()[0];
    art.append(h('div', { class: 'pg-cover' }, cv ? h('img', { src: cv.src, alt: '' }) : null));
    const lg = logo.photos()[0];
    const meta = [picker.titles().join(', '), cityIn.value.trim()].filter(Boolean).join(' · ');
    art.append(h('header', { class: 'pg-head' },
      lg ? h('img', { class: 'pg-logo', src: lg.thumb, alt: '' }) : h('span', { class: 'pg-logo is-none' }, (titleIn.value.trim() || '?').slice(0, 1).toUpperCase()),
      h('div', { class: 'pg-who' }, h('h1', { class: 'pg-t' }, titleIn.value.trim() || (kind === 'creator' ? 'Имя автора' : 'Название')), meta ? h('p', { class: 'pg-meta' }, meta) : null)));
    for (const b of blocks.value()) {
      const auto = b.type === 'channels' || b.type === 'reviews' || b.type === 'card';
      if (!auto && !filled(b)) continue;
      const head = b.title || (b.type === 'about' && kind === 'creator' ? PAGE_ABOUT_CREATOR : b.type === 'text' ? '' : PAGE_BLOCKS[b.type].title);
      const sec = h('section', { class: 'pg-b' }, head ? h('h2', { class: 'pg-h' }, head) : null);
      if (b.text) sec.append(h('p', { class: 'pg-text' }, b.text));
      if (b.type === 'facts') {
        const ul = h('ul', { class: 'pg-facts' });
        b.items.forEach((x) => ul.append(h('li', { html: ic('check', 18) }, x)));
        sec.append(ul);
      }
      if (b.type === 'topics') {
        const ch = h('div', { class: 'chips pg-chips' });
        blocks.topicTitles().forEach((x) => ch.append(h('span', { class: 'chip is-static' }, x)));
        sec.append(ch);
      }
      if (b.photos.length) {
        const g = h('div', { class: `pg-ph ${b.photos.length === 1 ? 'is-one' : ''}` });
        b.photos.forEach((p) => g.append(h('span', { class: 'lb-t' }, h('img', { src: p.thumb, alt: '', loading: 'lazy' }))));
        sec.append(g);
      }
      b.videos.forEach((v) => {
        if (v.src) sec.append(h('div', { class: 'pg-video' }, h('video', { src: v.src, poster: v.poster?.src, controls: true, preload: 'metadata', playsinline: true })));
        else if (v.url) sec.append(h('div', { class: 'pg-video is-link', html: ic(/vk|vkvideo/i.test(v.url) ? 'video' : 'youtube', 32) }, h('span', {}, 'Видео по ссылке — плеер площадки на странице')));
      });
      if (b.links.length) {
        const ul = h('ul', { class: 'pg-links' });
        b.links.forEach((l) => ul.append(h('li', {}, h('span', { html: ic('link', 16) }, l.replace(/^https?:\/\//, '')))));
        sec.append(ul);
      }
      const c = b.contacts;
      if (b.type === 'contacts' && c) {
        const dl = h('dl', { class: 'terms pg-contacts' });
        const row = (dt: string, dd: string) => dd && dl.append(h('div', {}, h('dt', {}, dt), h('dd', {}, dd)));
        row('Город', c.city);
        row('Адрес', c.address);
        row('Часы', c.hours);
        row('Телефон', c.phone);
        row('Сайт', c.site.replace(/^https?:\/\//, ''));
        row('Соцсети', c.socials.map((s) => s.replace(/^https?:\/\//, '')).join(', '));
        sec.append(dl);
      }
      if (auto) sec.append(h('p', { class: 'muted' }, b.type === 'reviews' ? 'Отзывов пока нет — они появятся после первых сделок.' : b.type === 'card' ? 'Здесь — ваша опубликованная анкета.' : 'Здесь — подтверждённые соцсети с цифрами от площадки.'));
      art.append(sec);
    }
    return art;
  }

  check();
  ready = true;
  form.dataset.ready = '1';
  if (draft) changed();
}
