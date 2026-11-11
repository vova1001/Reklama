// «Соцсети» в анкете автора: у каждой площадки — статус и одна кнопка. Цифры — только от площадки (сервер, задача 12):
// Telegram — бот-админ канала или группы (ссылка на бота → «Привязать» → добавить бота; отметка появляется сама),
// YouTube — разрешение Google. VK, TikTok, Instagram, Threads — «Скоро». У аккаунта — «Проверить снова» и «Отвязать».
import type { components } from '../../lib/api.gen';
import { api, busy } from '../../lib/client';
import { confirmDialog, guard, toast } from '../../lib/ui';
import { followersText as followers, kindTitle, updatedText as updated } from '../../lib/format';
import { PLATFORM_TITLE, SOCIAL_RESULT } from '../../content/app';
import { h, ic, type IconName } from './dom';

type Overview = components['schemas']['SocialOverview'];
type PlatformView = Overview['platforms'][number];
type Account = components['schemas']['SocialAccount'];

const ICON: Record<string, IconName> = { telegram: 'send', youtube: 'youtube', vk: 'users', tiktok: 'video', instagram: 'instagram', threads: 'atSign' };
export { followersText as followers, kindTitle } from '../../lib/format';

export class SocialList {
  private views: PlatformView[] = [];
  private poll = 0;
  private pollUntil = 0;
  private open = new Set<string>(); // площадки с раскрытой инструкцией
  private links = new Map<string, string>(); // ссылка на бота (Telegram)

  constructor(
    private root: HTMLElement,
    private onChange: () => void,
    private beforeLeave: () => Promise<unknown>,
  ) {
    void this.load().then(() => this.result());
  }

  /** Подтверждённые аккаунты (для проверки полноты и предпросмотра). */
  verified(): (Account & { platform: string })[] {
    return this.views.flatMap((v) => v.accounts.filter((a) => a.status === 'verified').map((a) => ({ ...a, platform: v.platform })));
  }

  /** Куда поставить курсор при «Осталось: соцсети». */
  focusTarget(): HTMLElement | null {
    return this.root.querySelector<HTMLElement>('[data-act]:not([aria-disabled="true"])');
  }

  private async load(): Promise<boolean> {
    const r = await api<Overview>('GET', '/api/me/social');
    if (!r.ok) {
      if (!guard(r.status) && !this.views.length) this.root.replaceChildren(h('li', { class: 'soc-s' }, 'Не удалось загрузить соцсети. Обновите страницу.'));
      return false;
    }
    const before = this.verified().length;
    this.views = r.data.platforms;
    this.render();
    this.root.dataset.verified = String(this.verified().length);
    if (this.verified().length !== before) this.onChange();
    return this.verified().length > before;
  }

  /** Итог возврата с площадки (?social=youtube&result=ok) — сообщением; адрес очищается. */
  private result() {
    const q = new URLSearchParams(location.search);
    const res = q.get('result');
    const p = q.get('social');
    if (!res || !p) return;
    const title = PLATFORM_TITLE[p] ?? 'Площадка';
    toast((SOCIAL_RESULT[res] ?? SOCIAL_RESULT.error).replace('{p}', title));
    q.delete('result');
    q.delete('social');
    history.replaceState(null, '', location.pathname + (q.size ? `?${q}` : '') + location.hash);
    this.root.closest('section')?.scrollIntoView({ block: 'start' });
  }

  private render() {
    this.root.replaceChildren(...this.views.map((v) => this.platform(v)));
  }

  private status(v: PlatformView): { text: string; cls: string } {
    if (!v.available) return { text: 'Скоро', cls: '' };
    const ok = v.accounts.filter((a) => a.status === 'verified').length;
    if (ok) return { text: ok > 1 ? `Подтверждено: ${ok}` : 'Подтверждено', cls: 'is-ok' };
    if (v.status === 'pending') return { text: 'Проверяем…', cls: '' };
    if (v.status === 'failed') return { text: 'Нужна повторная проверка', cls: 'is-err' };
    return { text: 'Не подключено', cls: '' };
  }

  private action(v: PlatformView): string {
    if (!v.available) return 'Скоро';
    if (v.accounts.some((a) => a.status === 'verified')) return v.platform === 'telegram' ? 'Добавить ещё' : 'Подключить ещё';
    if (v.status === 'failed') return 'Подтвердить заново';
    return 'Подключить';
  }

  private platform(v: PlatformView): HTMLElement {
    const st = this.status(v);
    const act = h('button', { class: 'btn pill-ghost soc-act', type: 'button', 'data-act': v.platform }, this.action(v));
    if (!v.available) {
      act.setAttribute('aria-disabled', 'true');
      act.dataset.tip = 'Подключение появится в ближайшем обновлении';
    }
    act.addEventListener('click', () => void this.start(v, act));
    const title = PLATFORM_TITLE[v.platform] ?? v.platform;
    const row = h('div', { class: 'soc-row' },
      h('span', { class: 'soc-ic', html: ic(ICON[v.platform] ?? 'link', 22) }),
      h('div', { class: 'soc-main' }, h('p', { class: 'soc-t' }, title), h('p', { class: `soc-s ${st.cls}` }, st.text)),
      act);
    const li = h('li', { class: 'soc-p', 'data-platform': v.platform }, row);
    if (this.open.has(v.platform)) li.append(this.steps(v));
    if (v.accounts.length) li.append(h('ul', { class: 'soc-accs' }, ...v.accounts.map((a) => this.account(v, a))));
    return li;
  }

  /** Инструкция Telegram в 3 строки + ссылка на бота. */
  private steps(v: PlatformView): HTMLElement {
    const link = this.links.get(v.platform) ?? '';
    return h('div', { class: 'soc-steps' },
      h('ol', {},
        h('li', {}, 'Откройте бота и нажмите «Привязать».'),
        h('li', {}, 'Добавьте бота в канал или группу администратором — права можно снять.'),
        h('li', {}, 'Отметка «Подтверждено» появится здесь сама.')),
      h('a', { class: 'btn pill-emerald soc-go', href: link, target: '_blank', rel: 'noopener' }, 'Открыть бота'));
  }

  private account(v: PlatformView, a: Account): HTMLElement {
    const name = a.title || (a.handle ? `@${a.handle}` : PLATFORM_TITLE[v.platform]);
    const t = h('p', { class: 'soc-at' }, a.url ? h('a', { href: a.url, target: '_blank', rel: 'noopener nofollow' }, name) : name);
    if (a.status === 'verified') t.append(h('span', { class: 'soc-badge', html: ic('badge', 16) }, 'Подтверждено'));
    const facts = a.status === 'failed'
      ? h('p', { class: 'soc-as is-err' }, a.failReason || 'Доступ потерян — подтвердите заново')
      : a.status === 'pending'
        ? h('p', { class: 'soc-as' }, 'Проверяем — обычно несколько минут')
        : h('p', { class: 'soc-as' }, [kindTitle(a.kind), followers(a), updated(a.refreshedAt)].filter(Boolean).join(' · '));
    const again = h('button', { class: 'ibtn', type: 'button', 'aria-label': 'Проверить снова', 'data-tip': 'Проверить снова', html: ic('refresh', 20) });
    const rm = h('button', { class: 'ibtn', type: 'button', 'aria-label': 'Отвязать', 'data-tip': 'Отвязать', html: ic('x', 20) });
    again.addEventListener('click', () => void this.refresh(a, again));
    rm.addEventListener('click', () => void this.unlink(a, name));
    return h('li', { class: 'soc-a', 'data-account': String(a.id) }, h('div', { class: 'soc-amain' }, t, facts), h('div', { class: 'soc-tools' }, again, rm));
  }

  private async start(v: PlatformView, btn: HTMLButtonElement) {
    if (!v.available || btn.classList.contains('is-busy')) return;
    busy(btn, true);
    const r = await api<{ url: string }>('POST', `/api/me/social/${v.platform}/start`);
    busy(btn, false);
    if (!r.ok) {
      if (!guard(r.status)) toast(r.error.message);
      return;
    }
    if (v.platform === 'telegram') {
      this.links.set(v.platform, r.data.url);
      this.open.add(v.platform);
      this.render();
      this.root.querySelector<HTMLAnchorElement>(`[data-platform="${v.platform}"] .soc-go`)?.focus();
      this.watch();
      return;
    }
    await this.beforeLeave(); // черновик анкеты — на сервер, затем на страницу разрешения площадки
    location.assign(r.data.url);
  }

  /** Ждём подтверждения из бота: опрос раз в 4 с до 30 минут, пока вкладка открыта. */
  private watch() {
    this.pollUntil = Date.now() + 30 * 60_000;
    if (this.poll) return;
    const tick = async () => {
      this.poll = 0;
      if (Date.now() > this.pollUntil) return;
      if (document.visibilityState === 'visible' && (await this.load())) {
        this.open.delete('telegram');
        this.render();
        toast('Telegram подтверждён — цифры взяли у Telegram');
        return;
      }
      this.poll = window.setTimeout(tick, 4000);
    };
    this.poll = window.setTimeout(tick, 4000);
  }

  private async refresh(a: Account, btn: HTMLButtonElement) {
    btn.setAttribute('aria-busy', 'true');
    const r = await api<{ account: Account }>('POST', `/api/me/social/accounts/${a.id}/refresh`);
    btn.removeAttribute('aria-busy');
    if (!r.ok) {
      if (!guard(r.status)) toast(r.error.message);
      return;
    }
    await this.load();
    toast(r.data.account.status === 'verified' ? 'Цифры обновлены' : 'Доступ потерян — подтвердите заново');
  }

  private async unlink(a: Account, name: string) {
    if (!(await confirmDialog('Отвязать?', `«${name}» пропадёт из анкеты. Подключить снова можно в любой момент.`, 'Отвязать'))) return;
    const r = await api<null>('DELETE', `/api/me/social/accounts/${a.id}`);
    if (!r.ok) {
      if (!guard(r.status)) toast(r.error.message);
      return;
    }
    await this.load();
  }
}
