// Тексты главной (макет «Охват», круг 4). То, что меняется (темы, тарифы, вопросы), — из API.
export const NAV = [
  { title: 'Бизнесу', href: '/#business' },
  { title: 'Авторам', href: '/#creators' },
  { title: 'Тарифы', href: '/#pricing' },
  { title: 'Вопросы', href: '/#faq' },
];

export const HERO = {
  // строки заголовка: перенос между ними — только с планшета
  title: [['Реклама', 'у', 'авторов', 'видео'], ['—', 'оплата', 'за', 'результат']],
  sub: ['Разместите задание с условиями.', 'Автор выполнит — сервис проверит и переведёт оплату.'],
  photos: [
    { file: 'cafe', icon: 'coffee', title: 'Кофейни' },
    { file: 'barber', icon: 'scissors', title: 'Барбершопы' },
    { file: 'florist', icon: 'flower-2', title: 'Цветочные' },
    { file: 'fitness', icon: 'dumbbell', title: 'Фитнес' },
  ],
};

export const STEPS = [
  { icon: 'wallet-cards', title: 'Выберите тариф', text: 'Срок размещения и место в&nbsp;ленте', chips: ['Бесплатный', 'Про', 'Максимум'] },
  { icon: 'list-plus', title: 'Опишите задание', text: 'Темы, город и ТЗ: что показать в&nbsp;ролике', chips: ['кофейни', 'Казань', 'ТЗ'] },
  { icon: 'target', title: 'Задайте условия', text: 'Просмотры, лайки и оплата. Пополните счёт', chips: ['10 000 просмотров', '300 лайков'] },
  { icon: 'badge-check', title: 'Автооплата', text: 'Автор выполнил — сервис проверил и&nbsp;оплатил', chips: null },
];

export const KINDS = [
  { icon: 'zap', title: 'Открытое', text: 'Любой автор берёт заказ сразу' },
  { icon: 'user-round-check', title: 'С одобрением', text: 'Заявки идут вам, автора выбираете сами' },
];

export const AUD = {
  biz: { icon: 'store', title: 'Для бизнеса' },
  aut: { icon: 'video', title: 'Для авторов' },
} as const;

// «С чего начать»: по строкам — бизнес слева, автор справа
export const GO = [
  { aud: 'biz', icon: 'list-plus', title: 'Разместить задание', text: 'Опишите условия, платите только за&nbsp;результат', href: '/tasks/new' },
  { aud: 'aut', icon: 'briefcase', title: 'Открытые задания', text: 'Берите заказ сразу, оплата после проверки', href: '/tasks' },
  { aud: 'biz', icon: 'users', title: 'Каталог авторов', text: 'Найдите авторов по теме и&nbsp;городу', href: '/creators' },
  { aud: 'aut', icon: 'user-round-pen', title: 'Анкета автора', text: 'Заполните один раз, получайте задания по&nbsp;своей&nbsp;теме', href: '/creators/new' },
] as const;

export const FOOTER = {
  about: 'Реклама у авторов видео с оплатой за результат: бизнес размещает задания, авторы выполняют, сервис проверяет.',
  mail: '[УТОЧНИТЬ: почта поддержки]',
  legal: '[УТОЧНИТЬ: юрлицо и ИНН]',
  cols: [
    { title: 'Бизнесу', links: [['Разместить задание', '/tasks/new'], ['Тарифы', '/pricing'], ['Каталог авторов', '/creators'], ['Как это работает', '/#business']] },
    { title: 'Авторам', links: [['Открытые задания', '/tasks'], ['Создать анкету', '/creators/new'], ['Тарифы для авторов', '/#creator-pricing']] },
    { title: 'Документы', links: [['Пользовательское соглашение', '/docs/terms'], ['Политика конфиденциальности', '/docs/privacy'], ['Согласие на обработку данных', '/docs/consent'], ['Правила площадки', '/docs/rules']] },
  ],
};
