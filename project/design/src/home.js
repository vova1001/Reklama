// «Охват» — главная, круг 4 финала (задача 13). Инструмент отрисовки эскиза, не код сайта.
// Круг 4: строгая система фонов (1 светлый фон + 1 цвет карточек, 1 тёмный фон + 1 цвет карточек, изумруд — только акцент),
// сетка 12 колонок (76 + 24), один шаг между блоками 144, все заголовки — по левому краю контейнера,
// ритм: тёмный первый экран → светлое (бизнесу) → тёмное (авторам) → светлое (темы, начало, вопросы) → тёмный финал с подвалом.
// Прошлые версии — home-v1.js, home-v2.js, home-v3.js.
const fs = require('fs'), path = require('path');

// ===== токены (README.md → «Токены») =====
const T = {
  bg: '#F3F2EE',      // светлый фон страницы — единственный светлый фон
  card: '#FAF9F6',    // карточка на светлом — единственная
  dark: '#1B1B1A',    // тёмный фон — единственный
  darkCard: '#262624',// карточка на тёмном — единственная
  ink: '#1B1B1A', onDark: '#EDECE8', gray: '#6F6E69', grayDark: '#9A9994',
  emerald: '#0F7553', // акцент: метка «Рекомендуем», «Оплачено», галочки
  col: 76, gut: 24, wrap: 1176, gap: 144, head: 64,
  r: { sheet: 44, card: 28, photo: 28 },
};
const { dark: D, darkCard: DC, onDark: TL, bg: BG, card: C, ink: K, gray: G, grayDark: GD, emerald: E } = T;
const kA = a => `rgba(27,27,26,${a})`, lA = a => `rgba(237,236,232,${a})`;
const span = n => n * T.col + (n - 1) * T.gut;

// ===== иконки Lucide =====
const ICONS = path.join(__dirname, 'icons'); const cache = {};
function ic(name, size = 24, sw = 1.6) {
  if (!cache[name]) cache[name] = fs.readFileSync(path.join(ICONS, name + '.svg'), 'utf8').replace(/<!--.*?-->/gs, '');
  let s = cache[name].replace(/<svg[^>]*>/s, m => m.replace(/\s(width|height|class|role)="[^"]*"/g, ''));
  s = s.replace('<svg', `<svg width="${size}" height="${size}" class="ic"`).replace(/stroke-width="2"/, `stroke-width="1.5"`);
  return s.replace(/\n\s*/g, ' ');
}

// ===== знак «два круга» (вариант 01) =====
const mark = (h, ring) => `<svg width="${h}" height="${h}" viewBox="0 0 48 48" aria-hidden="true">
<path d="M24 13.05A13 13 0 0 1 24 34.95A13 13 0 0 1 24 13.05Z" fill="${E}"/>
<circle cx="17" cy="24" r="13" fill="none" stroke="${ring}" stroke-width="5"/><circle cx="31" cy="24" r="13" fill="none" stroke="${ring}" stroke-width="5"/></svg>`;
const logo = c => `<a class="logo" style="color:${c}">${mark(40, c)}<b>охват</b></a>`;
const bigMark = (w, ring) => `<svg width="${w}" height="${Math.round(w * 26 / 44)}" viewBox="2 11 44 26" aria-hidden="true">
<path d="M24 13.765A12.4 12.4 0 0 1 24 34.235A12.4 12.4 0 0 1 24 13.765Z" fill="${E}"/>
<circle cx="17" cy="24" r="12.4" fill="none" stroke="${ring}" stroke-width=".55"/><circle cx="31" cy="24" r="12.4" fill="none" stroke="${ring}" stroke-width=".55"/></svg>`;

// ===== данные =====
const NAV = ['Бизнесу', 'Авторам', 'Тарифы', 'Вопросы'];
const PHOTOS = [['cafe', 'coffee', 'Кофейни'], ['barber', 'scissors', 'Барбершопы'], ['florist', 'flower-2', 'Цветочные'], ['fitness', 'dumbbell', 'Фитнес']];

const STEPS = [
  ['wallet-cards', 'Выберите тариф', 'Срок размещения и место в&nbsp;ленте', ['Бесплатный', 'Про', 'Максимум']],
  ['list-plus', 'Опишите задание', 'Темы, город и ТЗ: что показать в&nbsp;ролике', ['кофейни', 'Казань', 'ТЗ']],
  ['target', 'Задайте условия', 'Просмотры, лайки и оплата. Пополните счёт', ['10 000 просмотров', '300 лайков']],
  ['badge-check', 'Автооплата', 'Автор выполнил — сервис проверил и&nbsp;оплатил', null],
];

// Два вида заданий (ответ заказчика): открытое и с одобрением
const KINDS = [
  ['zap', 'Открытое', 'Любой автор берёт заказ сразу'],
  ['user-round-check', 'С одобрением', 'Заявки идут вам, автора выбираете сами'],
];

const BIZ = [
  ['Бесплатный', 'Попробовать сервис', ['Размещение 7 дней', 'Обычная карточка задания'], 0, '0 ₽'],
  ['Про', 'Для регулярной рекламы', ['Размещение 30 дней', 'Топ ленты 3 дня', 'Своё оформление карточки'], 1, '3 000 ₽'],
  ['Максимум', 'Охват и данные по максимуму', ['Размещение 60 дней', 'Топ ленты 7 дней', 'Статистика: просмотры по городам, цена за 1000 просмотров, сравнение авторов', 'Выгрузка отчётов по заданиям', 'Личный подбор авторов'], 0, '10 000 ₽'],
];
const AUT = [
  ['Бесплатный', 'Чтобы начать', ['10 откликов в месяц'], 0, '0 ₽'],
  ['Про', 'Для тех, кто снимает часто', ['50 откликов в месяц', 'Своё оформление профиля', 'Значок «Про» в каталоге'], 1, '3 000 ₽'],
  ['Топ', 'Больше всего заказов', ['Отклики без лимита', 'Место в топе каталога', 'Ранний доступ к новым заданиям'], 0, '10 000 ₽'],
];

const CATS = [
  ['coffee', 'Еда и кафе'], ['scissors', 'Красота и салоны'], ['dumbbell', 'Спорт и фитнес'], ['flower-2', 'Цветы и подарки'],
  ['shirt', 'Мода'], ['baby', 'Дети'], ['paw-print', 'Животные'], ['smartphone', 'Техника'],
  ['car', 'Авто'], ['house', 'Дом и ремонт'], ['graduation-cap', 'Образование'], ['plane', 'Путешествия'],
];
// «С чего начать» (задача 13): метка аудитории, заголовок-действие, одна строка пользы; колонки «Бизнесу» / «Авторам»
const GO = [
  ['biz', [['list-plus', 'Разместить задание', 'Опишите условия, платите только за&nbsp;результат'], ['users', 'Каталог авторов', 'Найдите авторов по теме и&nbsp;городу']]],
  ['aut', [['briefcase', 'Открытые задания', 'Берите заказ сразу, оплата после проверки'], ['user-round-pen', 'Анкета автора', 'Заполните один раз, получайте задания по&nbsp;своей&nbsp;теме']]],
];
const AUD = { biz: ['store', 'Для бизнеса'], aut: ['video', 'Для авторов'] };
const FAQ = [
  ['Как сервис проверяет выполнение?', 'Автор загружает ссылку на ролик. Сервис сверяет его с ТЗ и условиями — просмотры, лайки — и сам переводит оплату.'],
  ['Когда автор получает деньги?'],
  ['Что если условия не выполнены?'],
  ['Можно выбрать автора самому?'],
];

// ===== части =====
const arrow = () => `<span class="ar">${ic('arrow-up-right', 22, 1.8)}</span>`;
const plan = ([name, sub, items, hot, price], dark) => `<div class="plan ${hot ? 'plan-on' : ''}">
 <div class="pl-h"><h3>${name}</h3>${hot ? `<span class="rec">Рекомендуем</span>` : ''}</div>
 <p class="pl-s">${sub}</p>
 <div class="price">${price.replace(' ', '&nbsp;')}<small>/&nbsp;мес</small></div>
 <ul>${items.map(i => `<li>${ic('check', 22, 1.8)}<span>${i}</span></li>`).join('')}</ul>
 <a class="btn ${dark ? (hot ? 'b-d' : 'b-ol') : (hot ? 'b-l' : 'b-d')}">${price === '0 ₽' ? 'Начать бесплатно' : 'Выбрать ' + name}</a></div>`;
const head = (t, l) => `<div class="hd"><h2>${t}</h2>${l ? `<p class="lead">${l}</p>` : ''}</div>`;

module.exports = () => `<!doctype html><html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=1440">
<title>Охват — реклама у авторов видео с оплатой за результат</title>
<link rel="stylesheet" href="fonts/inter/inter.css"><style>
:root{--bg:${BG};--card:${C};--dark:${D};--dark-card:${DC};--ink:${K};--on-dark:${TL};--gray:${G};--gray-dark:${GD};--emerald:${E};
 --col:${T.col}px;--gut:${T.gut}px;--wrap:${T.wrap}px;--gap:${T.gap}px;--head:${T.head}px;--line:${kA(.1)};--line-dark:${lA(.1)}}
*{box-sizing:border-box;margin:0;padding:0}
body{width:1440px;font-family:'Inter',system-ui,sans-serif;font-optical-sizing:auto;font-size:18px;line-height:1.45;letter-spacing:-.01em;color:var(--ink);background:var(--bg);-webkit-font-smoothing:antialiased;text-rendering:geometricPrecision}
.w{width:var(--wrap);margin:0 auto;position:relative}
.g12{display:grid;grid-template-columns:repeat(12,var(--col));column-gap:var(--gut)}
a{color:inherit;text-decoration:none}
ul{list-style:none}
.ic{flex:none;display:block}
h1,h2{font-weight:650}
h2{font-size:60px;line-height:1.03;letter-spacing:-.04em}
h3{font-weight:600;letter-spacing:-.02em}
.lead{font-size:20px;line-height:1.45;color:var(--gray);margin-top:20px}
.on-dark .lead{color:var(--gray-dark)}
.blk+.blk{margin-top:var(--gap)}
.hd+*{margin-top:var(--head)}

/* зоны: светлая и тёмная, лист со скруглением наезжает на предыдущую */
.zone{padding:var(--gap) 0 calc(var(--gap) + ${T.r.sheet}px)}
.sheet{border-radius:${T.r.sheet}px ${T.r.sheet}px 0 0;margin-top:-${T.r.sheet}px;position:relative;z-index:1}
.light{background:var(--bg)}
.on-dark{background:var(--dark);color:var(--on-dark)}

/* поверхности: одна карточка на светлом, одна на тёмном; обводка 1px */
.cd{background:var(--card);box-shadow:inset 0 0 0 1px var(--line)}
.on-dark .cd{background:var(--dark-card);box-shadow:inset 0 0 0 1px var(--line-dark)}
.ring{border-radius:50%;display:flex;align-items:center;justify-content:center;flex:none;box-shadow:inset 0 0 0 1px var(--line)}
.on-dark .ring{box-shadow:inset 0 0 0 1px ${lA(.16)}}

/* кнопки-пилюли */
.btn{display:inline-flex;align-items:center;justify-content:center;gap:8px;height:52px;padding:0 26px;border-radius:999px;font-size:18px;font-weight:500;letter-spacing:-.01em;white-space:nowrap}
.b-l{background:var(--bg);color:var(--ink)}.b-d{background:var(--dark);color:var(--on-dark)}
.b-ol{box-shadow:inset 0 0 0 1px ${lA(.24)};color:var(--on-dark)}.b-od{box-shadow:inset 0 0 0 1px ${kA(.16)};color:var(--ink)}

/* шапка и первый экран */
.logo{display:flex;align-items:center;gap:10px}.logo b{font-size:29px;font-weight:700;letter-spacing:-.04em;line-height:1}
.bar{position:absolute;top:0;left:0;right:0;height:80px;z-index:5;background:rgba(27,27,26,.72);-webkit-backdrop-filter:saturate(180%) blur(20px);backdrop-filter:saturate(180%) blur(20px);border-bottom:1px solid ${lA(.08)}}
.nav{height:80px;display:grid;grid-template-columns:1fr auto 1fr;align-items:center}
.nav ul{display:flex;gap:36px;font-size:18px;color:${lA(.72)}}
.nav .r{display:flex;align-items:center;justify-content:flex-end;gap:24px;font-size:18px;color:${lA(.72)}}
.nav .btn{height:42px;padding:0 20px;font-size:17px}
.hero{padding:200px 0 calc(var(--gap) + ${T.r.sheet}px);position:relative}
.hero h1{text-align:center;font-size:92px;line-height:1.02;letter-spacing:-.045em;font-weight:680}
.hero h1 span{color:${lA(.5)}}
.hero .sub{text-align:center;font-size:23px;line-height:1.45;font-weight:400;letter-spacing:-.015em;color:var(--gray-dark);margin:32px auto 0;max-width:820px}
.hero .cta{display:flex;gap:12px;justify-content:center;margin-top:44px}
.pics{margin-top:104px;row-gap:var(--gut)}
.pic{grid-column:span 3;position:relative;height:400px;border-radius:${T.r.photo}px;overflow:hidden;background:var(--dark-card);box-shadow:0 0 0 1px ${lA(.08)}}
.pic img{width:100%;height:100%;object-fit:cover;display:block}
.pic span{position:absolute;left:16px;bottom:16px;display:inline-flex;align-items:center;gap:8px;height:42px;padding:0 16px 0 13px;border-radius:999px;background:rgba(27,27,26,.5);-webkit-backdrop-filter:saturate(160%) blur(16px);backdrop-filter:saturate(160%) blur(16px);box-shadow:inset 0 0 0 1px ${lA(.14)};color:var(--on-dark);font-size:18px;font-weight:500}

/* шаги */
.steps{row-gap:var(--gut)}
.st{grid-column:span 3;border-radius:${T.r.card}px;padding:32px 28px;display:flex;flex-direction:column;min-height:400px}
.st .top{display:flex;justify-content:space-between;align-items:center}
.st i{width:60px;height:60px}
.st .nx{color:${kA(.3)}}
.st h3{font-size:26px;line-height:1.15;letter-spacing:-.025em;margin-top:32px}
.st p{font-size:18px;color:var(--gray);margin-top:10px}
.chips{margin-top:auto;padding-top:24px;display:flex;flex-wrap:wrap;gap:8px}
.chip{height:40px;padding:0 16px;border-radius:999px;box-shadow:inset 0 0 0 1px ${kA(.12)};display:inline-flex;align-items:center;font-size:18px;font-weight:500}
.chip-ok{background:var(--emerald);color:var(--on-dark);box-shadow:none;gap:8px;padding-left:12px}

/* тарифы */
.plans{row-gap:var(--gut)}
.plan{grid-column:span 4;border-radius:${T.r.card}px;padding:40px;display:flex;flex-direction:column}
.pl-h{display:flex;justify-content:space-between;align-items:center;min-height:40px}
.plan h3{font-size:28px;letter-spacing:-.03em}
.rec{height:36px;padding:0 16px;border-radius:999px;background:var(--emerald);color:var(--on-dark);display:inline-flex;align-items:center;font-size:18px;font-weight:500}
.pl-s{font-size:18px;color:var(--gray);margin-top:8px}
.price{white-space:nowrap;font-size:56px;line-height:1;font-weight:600;letter-spacing:-.045em;font-variant-numeric:tabular-nums;font-feature-settings:'tnum' 1;margin-top:32px;padding-bottom:32px;border-bottom:1px solid var(--line)}
.price small{font-size:20px;font-weight:400;letter-spacing:-.01em;margin-left:10px;color:var(--gray)}
.plan ul{margin:28px 0 36px;display:flex;flex-direction:column;gap:16px}
.plan li{display:flex;gap:12px;font-size:18px;line-height:1.4}
.plan li .ic{color:var(--emerald);margin-top:1px}
.plan .btn{margin-top:auto;width:100%}
/* выделенный тариф — инверсия: на светлом тёмная карточка, на тёмном светлая */
.light .plan-on{background:var(--dark);color:var(--on-dark);box-shadow:none}
.light .plan-on .pl-s,.light .plan-on .price small{color:var(--gray-dark)}.light .plan-on .price{border-color:var(--line-dark)}.light .plan-on li .ic{color:var(--on-dark)}
.on-dark .pl-s,.on-dark .price small{color:var(--gray-dark)}.on-dark .price{border-color:var(--line-dark)}.on-dark li .ic{color:${lA(.8)}}
.on-dark .plan-on{background:var(--card);color:var(--ink);box-shadow:none}
.on-dark .plan-on .pl-s,.on-dark .plan-on .price small{color:var(--gray)}.on-dark .plan-on .price{border-color:var(--line)}.on-dark .plan-on li .ic{color:var(--emerald)}

/* два вида заданий */
.op{align-items:stretch}
.op-pic{grid-column:8 / span 5;grid-row:1;border-radius:${T.r.photo}px;overflow:hidden;min-height:580px}.op-pic img{width:100%;height:100%;object-fit:cover;display:block}
.op-l{grid-column:1 / span 6;grid-row:1;display:flex;flex-direction:column}
.kinds{display:grid;grid-template-columns:repeat(2,1fr);gap:var(--gut);margin:40px 0 var(--gut)}
.kd{border-radius:${T.r.card}px;padding:28px 28px 30px}
.kd i{width:52px;height:52px}
.kd b{display:block;font-size:24px;font-weight:600;letter-spacing:-.02em;margin-top:22px}
.kd span{display:block;font-size:18px;color:var(--gray-dark);margin-top:6px}
.ex{margin-top:auto;border-radius:${T.r.card}px;padding:28px;display:flex;align-items:center;justify-content:space-between;gap:20px}
.ex .tag{display:inline-flex;align-items:center;gap:8px;font-size:18px;color:var(--gray-dark)}
.ex b{display:block;font-size:24px;font-weight:600;letter-spacing:-.02em;margin-top:6px}
.ex .money{display:block;font-size:20px;margin-top:4px;color:${lA(.86)}}

/* категории */
.cgrid{row-gap:var(--gut)}
.cat{grid-column:span 3;height:76px;border-radius:999px;display:flex;align-items:center;gap:14px;padding:0 20px 0 12px;font-size:20px;font-weight:550;letter-spacing:-.02em;white-space:nowrap}
.cat i{width:52px;height:52px}
.more{margin-top:40px}

/* с чего начать: две колонки по 6 */
.gogrid{row-gap:var(--gut)}.gogrid .tile{grid-column:span 6}
.tile{border-radius:${T.r.card}px;padding:32px;display:grid;grid-template-columns:1fr auto;gap:0 24px;align-content:start;min-height:220px}
.aud{display:inline-flex;align-items:center;gap:8px;height:40px;padding:0 16px 0 12px;border-radius:999px;box-shadow:inset 0 0 0 1px ${kA(.14)};font-size:18px;font-weight:500;justify-self:start}
.tile .txt{grid-column:1 / -1;align-self:start;margin-top:56px}
.tile h3{font-size:34px;line-height:1.05;letter-spacing:-.035em;font-weight:650}
.tile p{font-size:20px;color:var(--gray);margin-top:10px}
.tile .ar{grid-column:2;grid-row:1}
.ar{width:52px;height:52px;border-radius:50%;display:inline-flex;align-items:center;justify-content:center;background:var(--dark);color:var(--on-dark);flex:none}

/* вопросы */
.faq{align-items:start}
.faq .side{grid-column:1 / span 4;display:flex;flex-direction:column;align-items:flex-start}
.faq .side .btn{margin-top:40px}
.qa{grid-column:6 / span 7;border-top:1px solid var(--line)}
.q{border-bottom:1px solid var(--line);padding:28px 0}
.q .h{display:flex;justify-content:space-between;align-items:center;gap:24px;font-size:22px;font-weight:600;letter-spacing:-.015em}
.q .pm{width:44px;height:44px}
.q-on .pm{background:var(--dark);color:var(--on-dark);box-shadow:none}
.q p{font-size:18px;color:var(--gray);margin-top:12px;max-width:560px}

/* финал: призыв + подвал на одном тёмном листе */
.start{align-items:center}
.start .txt{grid-column:1 / span 7}
.start h2{font-size:72px;line-height:1.02;letter-spacing:-.045em}
.start .lead{font-size:22px;margin-top:24px;max-width:540px}
.start .cta{display:flex;gap:12px;margin-top:44px}
.start .mk{grid-column:8 / span 5;display:flex;justify-content:flex-end}
.foot{padding-bottom:40px}
.foot .top{padding-top:var(--gap);border-top:1px solid var(--line-dark);row-gap:40px}
.foot .about{grid-column:span 4}.foot .col{grid-column:span 2}
.foot .about p{font-size:16px;line-height:1.5;color:var(--gray-dark);margin-top:24px;max-width:340px}
.foot .about .mail{display:inline-block;margin-top:24px;font-size:16px;border-bottom:1px solid ${lA(.28)};padding-bottom:4px}
.foot h4{font-size:14px;font-weight:500;color:${lA(.6)};margin-bottom:18px}
.foot .col a{display:block;font-size:16px;margin-bottom:12px}
.foot .bot{display:flex;justify-content:space-between;align-items:center;margin-top:88px;padding-top:28px;border-top:1px solid var(--line-dark);font-size:14px;color:${lA(.6)}}
.foot .bot b{color:var(--on-dark);font-weight:600}
</style></head><body>

<section class="hero on-dark"><header class="bar"><div class="w nav">${logo(TL)}<ul>${NAV.map(n => `<li>${n}</li>`).join('')}</ul>
  <div class="r"><span>Войти</span><a class="btn b-l">Разместить задание</a></div></div></header><div class="w">
 <h1>Реклама у авторов видео<br>— оплата за результат</h1>
 <p class="sub">Разместите задание с условиями.<br>Автор выполнит — сервис проверит и переведёт оплату.</p>
 <div class="cta"><a class="btn b-l">Разместить задание</a><a class="btn b-ol">Найти заказы</a></div>
 <div class="pics g12">${PHOTOS.map(([f, i, t]) => `<div class="pic"><img src="../assets/${f}.jpg" alt=""><span>${ic(i, 20, 1.7)}${t}</span></div>`).join('')}</div>
</div></section>

<section class="zone light sheet"><div class="w">
 <div class="blk how">${head('Как это работает<br>для бизнеса', 'Четыре шага — от тарифа до оплаты.')}
  <div class="steps g12">${STEPS.map(([i, t, d, chips], n) => `<div class="st cd"><div class="top"><i class="ring">${ic(i, 26, 1.6)}</i>${n < 3 ? `<span class="nx">${ic('arrow-right', 24, 1.6)}</span>` : ''}</div>
   <h3>${t}</h3><p>${d}</p><div class="chips">${chips ? chips.map(c => `<span class="chip">${c}</span>`).join('') : `<span class="chip chip-ok">${ic('check', 20, 2)}Оплачено автору</span>`}</div></div>`).join('')}</div></div>
 <div class="blk biz">${head('Тарифы для бизнеса', 'Платите за размещение, а автору — только за результат.')}
  <div class="plans g12">${BIZ.map(p => plan(p, 0).replace('class="plan ', 'class="cd plan ')).join('')}</div></div>
</div></section>

<section class="zone on-dark sheet"><div class="w">
 <div class="blk op g12"><div class="op-pic"><img src="../assets/creator.jpg" alt=""></div>
  <div class="op-l">${head('Два вида заданий', 'Деньги хранятся в безопасной сделке и&nbsp;уходят автору автоматически после проверки.')}
   <div class="kinds">${KINDS.map(([i, t, d]) => `<div class="kd cd"><i class="ring">${ic(i, 24, 1.6)}</i><b>${t}</b><span>${d}</span></div>`).join('')}</div>
   <div class="ex cd"><div><span class="tag">${ic('zap', 18, 1.7)}Кофейня · Казань</span><b>10 000 просмотров</b><span class="money">3 000 ₽ за выполнение</span></div><a class="btn b-l">Взять задание</a></div>
  </div></div>
 <div class="blk aut">${head('Тарифы для авторов', 'Больше откликов — больше заказов.')}
  <div class="plans g12">${AUT.map(p => plan(p, 1).replace('class="plan ', 'class="cd plan ')).join('')}</div></div>
</div></section>

<section class="zone light sheet"><div class="w">
 <div class="blk">${head('Задания в любой теме', 'Выберите свою — и смотрите заказы по ней.')}
  <div class="cgrid g12">${CATS.map(([i, t]) => `<a class="cat cd"><i class="ring">${ic(i, 24, 1.6)}</i>${t}</a>`).join('')}</div>
  <a class="btn b-d more">Все категории ${ic('arrow-right', 20, 1.8)}</a></div>

 <div class="blk go">${head('С чего начать', 'Выберите, кто вы, — и сразу к делу.')}
  <div class="gogrid g12">${[0, 1].flatMap(r => GO.map(([a, cards]) => [a, cards[r]])).map(([a, [i, t, d]]) => `<a class="tile cd"><span class="aud">${ic(AUD[a][0], 20, 1.7)}${AUD[a][1]}</span>${arrow()}<div class="txt"><h3>${t}</h3><p>${d}</p></div></a>`).join('')}</div></div>

 <div class="blk faq g12"><div class="side"><h2>Частые вопросы</h2><p class="lead">Не нашли ответ — напишите нам.</p><a class="btn b-od">Все вопросы</a></div>
  <div class="qa">${FAQ.map(([q, a]) => `<div class="q ${a ? 'q-on' : ''}"><div class="h">${q}<span class="pm ring">${ic(a ? 'minus' : 'plus', 20, 1.8)}</span></div>${a ? `<p>${a}</p>` : ''}</div>`).join('')}</div></div>
</div></section>

<footer class="foot on-dark sheet"><div class="w">
 <div class="start g12" style="padding:var(--gap) 0"><div class="txt"><h2>Разместите первое задание</h2>
   <p class="lead">Начните бесплатно — авторам платите только за&nbsp;результат.</p>
   <div class="cta"><a class="btn b-l">Разместить задание</a><a class="btn b-ol">Я автор</a></div></div>
  <div class="mk">${bigMark(400, TL)}</div></div>
 <div class="top g12"><div class="about">${logo(TL)}<p>Реклама у авторов видео с оплатой за результат: бизнес размещает задания, авторы выполняют, сервис проверяет.</p><a class="mail">[УТОЧНИТЬ: почта поддержки]</a></div>
  <div class="col"><h4>Бизнесу</h4><a>Разместить задание</a><a>Тарифы</a><a>Каталог авторов</a><a>Как это работает</a></div>
  <div class="col"><h4>Авторам</h4><a>Открытые задания</a><a>Создать анкету</a><a>Тарифы для авторов</a></div>
  <div class="col"><h4>Темы</h4><a>Еда и кафе</a><a>Красота и салоны</a><a>Спорт и фитнес</a><a>Цветы и подарки</a><a>Все категории</a></div>
  <div class="col"><h4>Документы</h4><a>Пользовательское соглашение</a><a>Политика конфиденциальности</a><a>Согласие на обработку данных</a><a>Правила площадки</a></div></div>
 <div class="bot"><span>© 2026 <b>Охват</b>. Все права защищены.</span><span>[УТОЧНИТЬ: юрлицо и ИНН]</span></div>
</div></footer>
</body></html>`;
