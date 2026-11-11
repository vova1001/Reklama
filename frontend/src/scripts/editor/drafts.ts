// Черновик формы: введённое не теряется при обновлении страницы и обрыве связи.
// Каждая правка — сразу копия в localStorage; через ~800 мс — на сервер (Redis, запасное — PostgreSQL). Нет связи —
// копия ждёт и уходит, как только связь вернётся. При открытии берётся самое свежее из двух.
import { api } from '../../lib/client';

export type DraftState = 'saving' | 'saved' | 'offline' | 'error';
export interface Saved<T> { data: T; savedAt: number } // savedAt — мс

interface Local<T> extends Saved<T> { synced: boolean }

const DEBOUNCE = 800;

export class DraftSync<T extends object> {
  private timer = 0;
  private inflight: Promise<void> | null = null;
  private pending: Local<T> | null = null;
  private lastSaved = 0;
  private tick = 0;

  constructor(
    private uid: number,
    private form: string,
    private onState: (s: DraftState, text: string) => void,
  ) {
    window.addEventListener('online', () => this.retry());
    this.tick = window.setInterval(() => this.lastSaved && this.onState('saved', savedText(this.lastSaved)), 30_000);
  }

  private get key() {
    return `ohvat:draft:${this.uid}:${this.form}`;
  }

  /** Самое свежее из копии на устройстве и на сервере (null — нет ни одной). */
  async load(): Promise<Saved<T> | null> {
    const local = this.readLocal();
    const r = await api<{ draft: { data: T; savedAt: string } | null }>('GET', `/api/me/drafts/${this.form}`, undefined, false, 3000);
    const remote = r.ok && r.data.draft ? { data: r.data.draft.data, savedAt: Date.parse(r.data.draft.savedAt) } : null;
    if (local && !local.synced && (!remote || local.savedAt >= remote.savedAt)) {
      this.pending = local; // не успела уйти на сервер — отправим
      this.schedule(0);
    }
    const best = [local, remote].filter((x): x is Saved<T> => !!x).sort((a, b) => b.savedAt - a.savedAt)[0] ?? null;
    if (best) this.lastSaved = best.savedAt;
    return best;
  }

  /** Правка: сразу на устройство, на сервер — после паузы. */
  touch(data: T) {
    const l: Local<T> = { data, savedAt: Date.now(), synced: false };
    this.writeLocal(l);
    this.pending = l;
    this.schedule(DEBOUNCE);
  }

  /** Отправить сейчас (перед уходом со страницы, перед публикацией). */
  flush(): Promise<void> {
    clearTimeout(this.timer);
    return this.push();
  }

  /** Форма получила постоянное имя (новое задание сохранилось): старый черновик — убрать. */
  async rename(form: string) {
    if (form === this.form) return;
    const old = this.form;
    const cur = this.readLocal();
    localStorage.removeItem(this.key);
    this.form = form;
    if (cur) this.writeLocal(cur);
    if (cur && !this.pending) this.pending = cur;
    void api('DELETE', `/api/me/drafts/${old}`);
    this.schedule(0);
  }

  /** Отправлено или удалено — черновик больше не нужен. */
  async clear() {
    clearTimeout(this.timer);
    this.pending = null;
    localStorage.removeItem(this.key);
    await api('DELETE', `/api/me/drafts/${this.form}`);
  }

  private retry() {
    if (this.pending) this.schedule(0);
  }

  private schedule(ms: number) {
    clearTimeout(this.timer);
    this.timer = window.setTimeout(() => void this.push(), ms);
  }

  private async push(): Promise<void> {
    if (this.inflight) {
      await this.inflight;
      return this.pending ? this.push() : undefined;
    }
    const p = this.pending;
    if (!p) return;
    this.pending = null;
    this.onState('saving', 'Сохраняю…');
    this.inflight = (async () => {
      const r = await api<{ savedAt: string }>('PUT', `/api/me/drafts/${this.form}`, p.data);
      if (r.ok) {
        this.lastSaved = Date.now();
        const cur = this.readLocal();
        if (cur && cur.savedAt === p.savedAt) this.writeLocal({ ...p, synced: true });
        this.onState('saved', savedText(this.lastSaved));
        return;
      }
      this.pending ??= p;
      if (r.status === 0) {
        this.onState('offline', 'Нет связи — сохранено на устройстве');
        this.schedule(5000);
      } else if (r.status === 429) {
        this.schedule((r.error.retryAfter ?? 2) * 1000);
      } else if (r.status === 413) {
        this.pending = null;
        this.onState('error', r.error.message);
      } else {
        this.onState('error', 'Сохранено на устройстве');
        this.schedule(10_000);
      }
    })();
    await this.inflight;
    this.inflight = null;
  }

  private readLocal(): Local<T> | null {
    try {
      const v = JSON.parse(localStorage.getItem(this.key) ?? 'null') as Local<T> | null;
      return v && typeof v.savedAt === 'number' && v.data && typeof v.data === 'object' ? v : null;
    } catch {
      return null;
    }
  }

  private writeLocal(v: Local<T>) {
    try {
      localStorage.setItem(this.key, JSON.stringify(v));
    } catch {
      /* место закончилось или запрещено — остаётся сервер */
    }
  }

  destroy() {
    clearInterval(this.tick);
  }
}

/** «Сохранено · только что», «· 5 мин назад», «· 14:30». */
export function savedText(at: number): string {
  const s = (Date.now() - at) / 1000;
  if (s < 60) return 'Сохранено · только что';
  if (s < 3600) return `Сохранено · ${Math.floor(s / 60)} мин назад`;
  const d = new Date(at);
  return `Сохранено · ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}
