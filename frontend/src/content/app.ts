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
  expired: 'Завершено',
  changes_requested: 'На доработке',
};

/** Поля, к которым модератор пишет замечания «на доработку» (как у сервера — service.FieldTitles). */
export const FIELD_TITLE: Record<string, string> = {
  general: 'Общее', title: 'Название', categories: 'Сфера', city: 'Город', formats: 'Формат', minViews: 'Просмотры',
  reward: 'Оплата', publishBy: 'Срок', blocks: 'Описание', cover: 'Обложка', photos: 'Фото', name: 'Имя', topics: 'Темы',
  platforms: 'Соцсети', avatar: 'Фото профиля', works: 'Работы',
};

/** Причины жалобы (как у сервера — domain.ComplaintReasons). */
export const COMPLAINT_REASONS: { id: string; title: string }[] = [
  { id: 'spam', title: 'Спам или реклама' },
  { id: 'fraud', title: 'Мошенничество' },
  { id: 'offensive', title: 'Оскорбления или травля' },
  { id: 'illegal', title: 'Запрещённые товары или услуги' },
  { id: 'fake', title: 'Фейк, чужие фото или накрутка' },
  { id: 'other', title: 'Другое' },
];

export const FORMATS: { id: string; title: string }[] = [
  { id: 'reels', title: 'Reels' },
  { id: 'shorts', title: 'Shorts' },
  { id: 'youtube', title: 'YouTube' },
  { id: 'vk_clips', title: 'VK Клипы' },
  { id: 'tiktok', title: 'TikTok' },
  { id: 'telegram', title: 'Telegram' },
];

/** Разделы форм: заголовок и подсказка в одну строку (до ~45 знаков — помещается на телефоне). */
export const FORM_GROUPS = {
  task: {
    main: { title: 'Основное', hint: 'По сфере, городу и формату вас найдут авторы' },
    terms: { title: 'Условия', hint: 'Кто берёт, сколько просмотров, оплата и срок' },
    desc: { title: 'Описание', hint: 'Что снять и что важно — читают первым' },
    photo: { title: 'Фото', hint: 'Примеры, что показать, — до 6 фото' },
  },
  card: {
    main: { title: 'Основное', hint: 'Как вас зовут, где вы и о чём снимаете' },
    platforms: { title: 'Соцсети', hint: 'Подтвердите аккаунт — цифры возьмём у площадки' },
    desc: { title: 'О себе', hint: 'По желанию: опыт, форматы и цены' },
    photo: { title: 'Работы', hint: 'Кадры из ваших видео — до 9 фото' },
  },
  profile: {
    basics: { title: 'Общие данные', hint: 'Видно в приложении: имя и город' },
    modes: { title: 'Режимы', hint: 'Заполните тот, в котором работаете' },
  },
} as const;

export const PLATFORM_TITLE: Record<string, string> = {
  telegram: 'Telegram', youtube: 'YouTube', vk: 'ВКонтакте', tiktok: 'TikTok', instagram: 'Instagram', threads: 'Threads',
};

/** Итог возврата с площадки (?result=…) — {p}: название площадки. Коды — как у сервера (/api/social/{p}/callback). */
export const SOCIAL_RESULT: Record<string, string> = {
  ok: '{p} подтверждён — цифры взяли у площадки',
  denied: 'Без разрешения {p} подтвердить нельзя. Нажмите «Подключить» и разрешите доступ',
  state: 'Подключение устарело. Нажмите «Подключить» ещё раз',
  no_channel: 'У этого аккаунта нет канала {p}. Выберите другой аккаунт',
  too_many: 'Соцсетей слишком много — отвяжите ненужные',
  limit: 'Подключений много. Попробуйте через час',
  disabled: 'Подключение {p} появится скоро',
  unavailable: '{p} не ответил. Попробуйте через минуту',
  error: 'Не получилось подключить. Попробуйте позже',
};

/** Шаблоны блоков: одним нажатием (заголовок можно поменять). */
export type BlockKind = 'text' | 'list' | 'links';
export const TASK_TEMPLATES: { title: string; kind: BlockKind }[] = [
  { title: 'Описание', kind: 'text' },
  { title: 'Что показать', kind: 'list' },
  { title: 'Требования', kind: 'list' },
  { title: 'Формат', kind: 'list' },
  { title: 'Сроки', kind: 'list' },
];
export const CARD_TEMPLATES: { title: string; kind: BlockKind }[] = [
  { title: 'О себе', kind: 'text' },
  { title: 'Форматы и цены', kind: 'list' },
  { title: 'Примеры работ', kind: 'links' },
];
/** Блоки нового задания и анкеты. */
export const TASK_BLOCKS = [
  { kind: 'text' as const, title: 'Описание', items: [] as string[] },
  { kind: 'list' as const, title: 'Что показать', items: [] as string[] },
];
export const CARD_BLOCKS = [{ kind: 'text' as const, title: 'О себе', items: [] as string[] }];

/** Ошибки полноты — те же тексты, что у сервера (service/tasks.go complete, service/cards.go cardComplete). */
export const TASK_RULES: Record<string, { short: string; msg: string }> = {
  title: { short: 'название', msg: 'Название — от 5 до 100 символов' },
  categories: { short: 'сфера', msg: 'Выберите сферу' },
  city: { short: 'город', msg: 'Укажите город или «Вся Россия»' },
  formats: { short: 'формат', msg: 'Выберите хотя бы один формат' },
  minViews: { short: 'просмотры', msg: 'Укажите число просмотров' },
  reward: { short: 'оплата', msg: 'Оплата — от 100 ₽' },
  publishBy: { short: 'срок', msg: 'Укажите срок публикации' },
  blocks: { short: 'описание', msg: 'Опишите задание: добавьте описание или требования' },
};
export const DUE_RANGE = 'Срок — от завтра до 90 дней';
export const CARD_RULES: Record<string, { short: string; msg: string }> = {
  name: { short: 'имя', msg: 'Имя — от 2 символов' },
  city: { short: 'город', msg: 'Укажите город' },
  topics: { short: 'темы', msg: 'Выберите хотя бы одну тему' },
  platforms: { short: 'соцсети', msg: 'Подтвердите хотя бы одну соцсеть' },
};

/** Способы входа. how — как подключить (только то, что уже работает). */
export const PROVIDERS = [
  { id: 'email', title: 'Почта', icon: 'mail', how: 'Войдите по коду на почту аккаунта' },
  { id: 'google', title: 'Google', icon: 'key-round', how: 'Войдите через Google с той же почтой' },
  { id: 'telegram', title: 'Telegram', icon: 'send', how: 'Привязка — в ближайшем обновлении' },
];
