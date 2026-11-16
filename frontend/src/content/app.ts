// Тексты приложения после входа. Коротко: подписи — 1–2 слова, пояснения — плейсхолдерами и иконками.
import type { Mode } from '../lib/auth';

export const MODES: { id: Mode; title: string; icon: string }[] = [
  { id: 'business', title: 'Бизнес', icon: 'store' },
  { id: 'creator', title: 'Автор', icon: 'video' },
];

export type Page = 'feed' | 'create' | 'my' | 'profile' | 'settings' | 'notifications';

/** Навигация: mode — пункт только для режима (Мои задания — у бизнеса, Анкета — у автора); rail — только в левой колонке. */
export const NAV: { id: Page; title: string; icon: string; href: string; mode?: Mode; rail?: boolean }[] = [
  { id: 'feed', title: 'Лента', icon: 'house', href: '/app' },
  { id: 'my', title: 'Мои задания', icon: 'list', href: '/app/tasks', mode: 'business' },
  { id: 'create', title: 'Анкета', icon: 'id-card', href: '/app/card', mode: 'creator' },
  { id: 'notifications', title: 'Уведомления', icon: 'bell-ring', href: '/app/notifications', rail: true },
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
  expired: 'Просрочено',
  changes_requested: 'На доработке',
  archived: 'В архиве',
};

/** Поля, к которым модератор пишет замечания «на доработку» (как у сервера — service.FieldTitles). */
export const FIELD_TITLE: Record<string, string> = {
  general: 'Общее', title: 'Название', categories: 'Сфера', city: 'Город', formats: 'Формат', minViews: 'Просмотры',
  reward: 'Оплата', publishBy: 'Срок', blocks: 'Описание', cover: 'Обложка', photos: 'Фото', name: 'Имя', topics: 'Темы',
  platforms: 'Соцсети', avatar: 'Фото профиля', works: 'Работы', spheres: 'Сферы', logo: 'Логотип', slug: 'Адрес страницы',
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

/** Крупные города — подсказки в поле «Город» (ввод свободный). */
export const CITIES = [
  'Москва', 'Санкт-Петербург', 'Новосибирск', 'Екатеринбург', 'Казань', 'Нижний Новгород', 'Самара', 'Омск',
  'Челябинск', 'Магнитогорск', 'Ростов-на-Дону', 'Уфа', 'Красноярск', 'Воронеж', 'Пермь', 'Волгоград',
  'Краснодар', 'Саратов', 'Тюмень', 'Тольятти', 'Ижевск', 'Барнаул', 'Ульяновск', 'Иркутск', 'Хабаровск',
  'Ярославль', 'Владивосток', 'Махачкала', 'Томск', 'Оренбург', 'Кемерово', 'Рязань', 'Астрахань', 'Сочи',
];

/** Тип бизнеса — первый шаг мастера задания; ведёт выбор цели. */
export const BIZ_TYPES: { id: string; title: string; tip: string; icon: string }[] = [
  { id: 'offline', title: 'Офлайн-бизнес', tip: 'Кафе, магазин, салон — есть точка, куда приходят люди', icon: 'store' },
  { id: 'online', title: 'Онлайн-бизнес', tip: 'Магазин или сервис в интернете — заказ без визита', icon: 'globe' },
  { id: 'services', title: 'Услуги в интернете', tip: 'Вы продаёте свои услуги и ищете клиентов онлайн', icon: 'monitor-smartphone' },
];

/** Цель задания: под неё считается условие выполнения. kinds — для каких типов бизнеса доступна;
 *  about — плашка-пояснение при выборе; link — просить реферальную ссылку или промокод; nums — поля «число + оплата». */
export const GOALS: { id: string; title: string; tip: string; label: string; short: string; kinds: string[]; icon: string; about: string; link?: boolean; nums?: boolean }[] = [
  {
    id: 'views', title: 'Просмотры', tip: 'Ролик набрал нужное число просмотров', label: 'Просмотров от', short: 'просмотры',
    kinds: ['offline', 'online', 'services'], icon: 'eye', nums: true,
    about: 'Автор снимает ролик про вас. Как только он набрал нужное число просмотров — задание выполнено, оплата уходит автору. Число мы проверяем сами по площадке.',
  },
  {
    id: 'subscribers', title: 'Подписчики', tip: 'Пришли новые подписчики на ваш аккаунт', label: 'Подписчиков от', short: 'подписчики',
    kinds: ['offline', 'online', 'services'], icon: 'users', nums: true, link: true,
    about: 'Автор зовёт подписаться на вас. Укажите ссылку на аккаунт — авторы поставят её в ролик, а мы сверим прирост подписчиков по площадке.',
  },
  {
    id: 'clicks', title: 'Переходы', tip: 'Люди перешли по вашей ссылке или промокоду', label: 'Переходов от', short: 'переходы',
    kinds: ['offline', 'online', 'services'], icon: 'link', nums: true, link: true,
    about: 'Дайте реферальную ссылку или промокод — авторы разместят их у себя. Переходы и покупки по ним видны и вам, и нам: платите только за реальный результат.',
  },
  {
    id: 'guests', title: 'Гости', tip: 'Реальные люди пришли на вашу точку', label: 'Гостей от', short: 'гости',
    kinds: ['offline'], icon: 'map-pin', nums: true, link: true,
    about: 'Люди приходят на вашу точку и называют промокод или показывают ролик. Вы отмечаете каждого — считаем вместе, без накруток.',
  },
  {
    id: 'barter', title: 'Бартер', tip: 'Товар или услуга вместо денег', label: '', short: 'бартер',
    kinds: ['offline', 'online', 'services'], icon: 'gift',
    about: 'Без денег: автор получает ваш товар, блюдо или услугу, а взамен снимает и публикует ролик. В описании напишите, что именно предлагаете. Онлайн-бизнес — отправьте товар на обзор.',
  },
];

/** Разделы форм: заголовок и подсказка в одну строку (до ~45 знаков — помещается на телефоне). */
export const FORM_GROUPS = {
  task: {
    main: { title: 'Основное', hint: 'По сфере, городу и формату вас найдут авторы' },
    terms: { title: 'Условия', hint: 'Кто берёт, цель, оплата и срок' },
    desc: { title: 'Описание', hint: 'Что снять и что важно — читают первым' },
    photo: { title: 'Фото и видео', hint: 'Примеры, что показать, — до 6 фото и 2 видео' },
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
  biz: { short: 'тип бизнеса', msg: 'Выберите, кто вы' },
  title: { short: 'название', msg: 'Название — от 5 до 100 символов' },
  categories: { short: 'сфера', msg: 'Выберите сферу' },
  city: { short: 'город', msg: 'Укажите город или «Вся Россия»' },
  formats: { short: 'формат', msg: 'Выберите хотя бы один формат' },
  link: { short: 'ссылка', msg: 'Укажите ссылку или промокод — по ним считаются переходы' },
  minViews: { short: 'цель', msg: 'Укажите целевое число' },
  reward: { short: 'оплата', msg: 'Оплата — от 100 ₽' },
  publishBy: { short: 'срок', msg: 'Укажите срок публикации' },
  blocks: { short: 'описание', msg: 'Опишите задание: добавьте описание или требования' },
};
export const DUE_RANGE = 'Срок — от завтра до 90 дней';

/** Шаги мастера задания (бизнес). */
export const TASK_STEPS = [
  { id: 'biz', title: 'Кто вы', hint: 'Под тип бизнеса подберём цели' },
  { id: 'goal', title: 'Что нужно', hint: 'Выберите результат — условия под него' },
  { id: 'where', title: 'Где и когда', hint: 'Сфера, город, форматы и срок' },
  { id: 'look', title: 'Оформление', hint: 'То, что увидят авторы первым' },
];
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

// ===== страница бизнеса и профиль автора (задача 14) =====

export type PageKind = 'business' | 'creator';
export type PageBlockType = 'about' | 'photos' | 'video' | 'contacts' | 'facts' | 'text' | 'channels' | 'topics' | 'works' | 'reviews' | 'card';

/** Пути публичных страниц. */
export const PAGE_PATH: Record<PageKind, string> = { business: '/b/', creator: '/c/' };
export const PAGE_TITLE: Record<PageKind, string> = { business: 'Страница бизнеса', creator: 'Профиль автора' };

/** Блоки страницы: название, иконка, подсказка в списке «Добавить блок». Как у сервера (domain.PageBlockTypes). */
export const PAGE_BLOCKS: Record<PageBlockType, { title: string; icon: string; hint: string }> = {
  about: { title: 'О компании', icon: 'text', hint: 'Кто вы и чем полезны' },
  photos: { title: 'Фото', icon: 'image-plus', hint: 'Галерея — до 12 фото' },
  video: { title: 'Видео', icon: 'video', hint: 'Файл MP4 или ссылка YouTube / VK' },
  contacts: { title: 'Адрес и контакты', icon: 'map-pin', hint: 'Город, адрес, часы, сайт, соцсети' },
  facts: { title: 'Факты', icon: 'list-checks', hint: 'Короткие пункты: цифры, награды' },
  text: { title: 'Заголовок + текст', icon: 'text', hint: 'Свой раздел' },
  channels: { title: 'Мои каналы', icon: 'badge-check', hint: 'Подтверждённые соцсети — сами' },
  topics: { title: 'Что могу рекламировать', icon: 'tag', hint: 'Темы, с которыми работаете' },
  works: { title: 'Примеры работ', icon: 'camera', hint: 'Фото, видео и ссылки' },
  reviews: { title: 'Отзывы', icon: 'heart', hint: 'Появятся после первых сделок' },
  card: { title: 'Анкета', icon: 'id-card', hint: 'Услуги, форматы и цены — из анкеты' },
};
export const PAGE_ABOUT_CREATOR = 'О себе';
export const PAGE_TYPES: Record<PageKind, PageBlockType[]> = {
  business: ['about', 'photos', 'video', 'contacts', 'facts', 'text'],
  creator: ['about', 'channels', 'topics', 'works', 'reviews', 'card', 'text', 'photos', 'video', 'facts'],
};
export const PAGE_SINGLE: PageBlockType[] = ['about', 'contacts', 'channels', 'topics', 'works', 'reviews', 'card'];
/** Блоки новой страницы. */
export const PAGE_START: Record<PageKind, PageBlockType[]> = {
  business: ['about', 'photos', 'contacts'],
  creator: ['about', 'channels', 'topics', 'works', 'card'],
};

/** Ошибки полноты — те же тексты, что у сервера (service/pages_check.go pageComplete). */
export const PAGE_RULES: Record<PageKind, Record<string, { short: string; msg: string }>> = {
  business: {
    title: { short: 'название', msg: 'Название — от 2 символов' },
    city: { short: 'город', msg: 'Укажите город' },
    spheres: { short: 'сфера', msg: 'Выберите хотя бы одну сферу' },
    blocks: { short: 'блоки', msg: 'Добавьте хотя бы один блок с содержимым' },
  },
  creator: {
    title: { short: 'имя', msg: 'Имя — от 2 символов' },
    city: { short: 'город', msg: 'Укажите город' },
    spheres: { short: 'темы', msg: 'Выберите хотя бы одну тему' },
    blocks: { short: 'блоки', msg: 'Добавьте хотя бы один блок с содержимым' },
  },
};

export const PAGE_GROUPS = {
  address: { title: 'Адрес', hint: 'Ссылка на страницу — латиница, цифры и дефис' },
  head: { title: 'Шапка', hint: 'Обложка, логотип, название, сферы и город' },
  blocks: { title: 'Блоки', hint: 'Добавляйте, перетаскивайте, удаляйте' },
} as const;
