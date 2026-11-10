// Тексты приложения после входа. Задания в ленте — примеры (настоящих данных пока нет), помечены «Пример».
import type { Mode } from '../lib/auth';

export const MODES: { id: Mode; title: string; icon: string; hint: string }[] = [
  { id: 'business', title: 'Бизнес', icon: 'store', hint: 'Размещаю задания' },
  { id: 'creator', title: 'Автор', icon: 'video', hint: 'Снимаю ролики' },
];

export const FEED = {
  business: { title: 'Ваши задания', sub: 'Опишите, что снять, — авторы из вашего города откликнутся.' },
  creator: { title: 'Задания для вас', sub: 'Задания бизнеса по вашим темам и городу.' },
};

/** Мягкая подсказка, если профиль активного режима не заполнен (не блокирует). */
export const NUDGE: Record<Mode, { title: string; text: string; cta: string }> = {
  business: { title: 'Заполните профиль бизнеса', text: 'Название, город и сфера — чтобы авторы понимали, с кем работают.', cta: 'Заполнить' },
  creator: { title: 'Заполните профиль автора, чтобы откликаться', text: 'Город, темы и ссылки на каналы — бизнес увидит их в отклике.', cta: 'Заполнить' },
};

export const DEMO_TASKS = [
  {
    brand: 'Кофейня «Зерно»', city: 'Казань', sphere: 'Еда и кафе', icon: 'coffee',
    title: 'Сторис о новом осеннем меню', format: 'Вертикальное видео до 60 с', budget: '3 000 ₽', goal: 'от 5 000 просмотров', due: 'до 20 ноября',
  },
  {
    brand: 'Барбершоп «Борода»', city: 'Казань', sphere: 'Красота', icon: 'scissors',
    title: 'Ролик «до и после» стрижки', format: 'Reels или Shorts', budget: '4 500 ₽', goal: 'от 8 000 просмотров', due: 'до 25 ноября',
  },
  {
    brand: 'Студия растяжки «Гибко»', city: 'Казань', sphere: 'Спорт', icon: 'dumbbell',
    title: 'Пробное занятие глазами новичка', format: 'Видео 1–2 минуты', budget: '6 000 ₽', goal: 'от 12 000 просмотров', due: 'до 1 декабря',
  },
];

export const DEMO_OWN_TASK = {
  title: 'Сторис о новом осеннем меню', status: 'Идут отклики', responses: 4, budget: '3 000 ₽', goal: 'от 5 000 просмотров',
};

export const HOW: Record<Mode, string[]> = {
  business: ['Опишите задание и условия оплаты', 'Выберите автора из откликов', 'Платите только за результат'],
  creator: ['Выберите задание по своей теме', 'Снимите и опубликуйте ролик', 'Получите оплату после проверки'],
};

export const ACTIONS: Record<Mode, { title: string; href: string; icon: string }[]> = {
  business: [
    { title: 'Создать задание', href: '/tasks/new', icon: 'list-plus' },
    { title: 'Найти авторов', href: '/creators', icon: 'users' },
    { title: 'Тарифы бизнеса', href: '/pricing', icon: 'wallet-cards' },
  ],
  creator: [
    { title: 'Все задания', href: '/tasks', icon: 'list-plus' },
    { title: 'Мои каналы', href: '/app/profile?tab=creator', icon: 'link' },
    { title: 'Тарифы авторов', href: '/pricing', icon: 'wallet-cards' },
  ],
};

export const TIPS: Record<Mode, string> = {
  business: 'Задания с понятным результатом («от 5 000 просмотров») получают больше откликов.',
  creator: 'Добавьте 2–3 канала: бизнес чаще выбирает авторов с несколькими площадками.',
};

export const STEP_TITLE: Record<string, (mode: Mode) => { title: string; href: string }> = {
  basics: () => ({ title: 'Имя и город', href: '/app/profile' }),
  profile: (m) => ({ title: m === 'creator' ? 'Профиль автора' : 'Профиль бизнеса', href: `/app/profile?tab=${m}` }),
  telegram: () => ({ title: 'Уведомления в Telegram', href: '/app/settings#notify' }),
};

/** Способы входа. how — как подключить (только то, что уже работает): совпадение подтверждённой почты связывает способы. */
export const PROVIDERS = [
  { id: 'email', title: 'Почта', icon: 'mail', how: 'Войдите по коду на почту аккаунта — способ добавится сам.' },
  { id: 'google', title: 'Google', icon: 'key-round', how: 'Войдите через Google с той же почтой — способ добавится сам.' },
  { id: 'telegram', title: 'Telegram', icon: 'send', how: 'Привязка Telegram к аккаунту появится здесь в ближайшем обновлении.' },
];
