// Тексты приложения после входа. Коротко: подписи — 1–2 слова, пояснения — плейсхолдерами и иконками.
import type { Mode } from '../lib/auth';

export const MODES: { id: Mode; title: string; icon: string }[] = [
  { id: 'business', title: 'Бизнес', icon: 'store' },
  { id: 'creator', title: 'Автор', icon: 'video' },
];

export type Page = 'feed' | 'create' | 'my' | 'profile' | 'settings';

/** Навигация: mode — пункт только для режима (Создать и Мои — у бизнеса, Анкета — у автора). */
export const NAV: { id: Page; title: string; icon: string; href: string; mode?: Mode }[] = [
  { id: 'feed', title: 'Лента', icon: 'house', href: '/app' },
  { id: 'create', title: 'Создать', icon: 'square-plus', href: '/app/tasks/new', mode: 'business' },
  { id: 'my', title: 'Мои', icon: 'list', href: '/app/tasks', mode: 'business' },
  { id: 'create', title: 'Анкета', icon: 'id-card', href: '/app/card', mode: 'creator' },
  { id: 'profile', title: 'Профиль', icon: 'user-round', href: '/app/profile' },
  { id: 'settings', title: 'Настройки', icon: 'settings-2', href: '/app/settings' },
];

/** Тонкая строка вверху ленты, если профиль режима не заполнен (закрывается). */
export const NUDGE: Record<Mode, string> = {
  business: 'Заполните профиль бизнеса',
  creator: 'Заполните профиль автора',
};

export const STATUS: Record<string, string> = {
  draft: 'Черновик',
  pending_review: 'На проверке',
  published: 'Опубликовано',
  rejected: 'Отклонено',
};

export const FORMATS: { id: string; title: string }[] = [
  { id: 'reels', title: 'Reels' },
  { id: 'shorts', title: 'Shorts' },
  { id: 'youtube', title: 'YouTube' },
  { id: 'vk_clips', title: 'VK Клипы' },
  { id: 'tiktok', title: 'TikTok' },
  { id: 'telegram', title: 'Telegram' },
];

export const PLATFORM_TITLE: Record<string, string> = {
  telegram: 'Telegram', youtube: 'YouTube', vk: 'ВКонтакте', tiktok: 'TikTok', instagram: 'Instagram',
  dzen: 'Дзен', rutube: 'Rutube', other: 'Сайт',
};

/** Блоки по умолчанию (заголовки можно менять, блоки — убирать и добавлять). */
export const TASK_BLOCKS = [
  { kind: 'text', title: 'Описание', items: [] as string[] },
  { kind: 'list', title: 'Что показать', items: [] as string[] },
  { kind: 'list', title: 'Требования', items: [] as string[] },
];
export const CARD_BLOCKS = [
  { kind: 'text', title: 'О себе', items: [] as string[] },
  { kind: 'list', title: 'Форматы и цены', items: [] as string[] },
  { kind: 'links', title: 'Примеры работ', items: [] as string[] },
];

/** Способы входа. how — как подключить (только то, что уже работает). */
export const PROVIDERS = [
  { id: 'email', title: 'Почта', icon: 'mail', how: 'Войдите по коду на почту аккаунта' },
  { id: 'google', title: 'Google', icon: 'key-round', how: 'Войдите через Google с той же почтой' },
  { id: 'telegram', title: 'Telegram', icon: 'send', how: 'Привязка — в ближайшем обновлении' },
];
