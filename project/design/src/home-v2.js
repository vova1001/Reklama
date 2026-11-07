// «Охват» — главная, круг 2 финала (задача 12). Инструмент отрисовки эскиза, не код сайта.
// Стилистика как у референса: тёмный верх с шапкой → светлое полотно со скруглением 44 → большие панели r40,
// крупные заголовки Roboto Flex (wdth 114), пилюли, иконки Lucide. Без телефонов.
// Круг 2: мягче контраст (тёплый тёмный и кремовый, без чисто белого и чисто чёрного), 5 фото, модель сервиса.
// Прошлая версия — home-v1.js.
const fs = require('fs'), path = require('path');

// ===== токены =====
const T = {
  dark: '#1B1B1A', darkCard: '#262624', onDark: '#EDECE8', btnLight: '#ECEBE6',
  bg: '#F3F2EE', card: '#FAF9F6', ink: '#1B1B1A', gray: '#6B6A65', emerald: '#12805C',
  r: { sheet: 44, panel: 40, tile: 32, card: 28, photo: 28 }, wrap: 1176,
};
const { dark: D, darkCard: DC, onDark: TL, btnLight: BL, bg: BG, card: C, ink: K, gray: G, emerald: E } = T;
const kA = a => `rgba(27,27,26,${a})`, lA = a => `rgba(237,236,232,${a})`;

// ===== иконки Lucide =====
const ICONS = path.join(__dirname, 'icons'); const cache = {};
function ic(name, size = 24, sw = 1.6) {
  if (!cache[name]) cache[name] = fs.readFileSync(path.join(ICONS, name + '.svg'), 'utf8').replace(/<!--.*?-->/gs, '');
  let s = cache[name].replace(/<svg[^>]*>/s, m => m.replace(/\s(width|height|class|role)="[^"]*"/g, ''));
  s = s.replace('<svg', `<svg width="${size}" height="${size}" class="ic"`).replace(/stroke-width="2"/, `stroke-width="${sw}"`);
  return s.replace(/\n\s*/g, ' ');
}

// ===== знак «два круга» (вариант 01) =====
const mark = (h, ring) => `<svg width="${h}" height="${h}" viewBox="0 0 48 48" aria-hidden="true">
<path d="M24 13.05A13 13 0 0 1 24 34.95A13 13 0 0 1 24 13.05Z" fill="${E}"/>
<circle cx="17" cy="24" r="13" fill="none" stroke="${ring}" stroke-width="5"/><circle cx="31" cy="24" r="13" fill="none" stroke="${ring}" stroke-width="5"/></svg>`;
const logo = c => `<a class="logo" style="color:${c}">${mark(40, c)}<b>охват</b></a>`;
const bigMark = w => `<svg width="${w}" height="${Math.round(w * 26 / 44)}" viewBox="2 11 44 26" aria-hidden="true">
<path d="M24 13.765A12.4 12.4 0 0 1 24 34.235A12.4 12.4 0 0 1 24 13.765Z" fill="${E}"/>
<circle cx="17" cy="24" r="12.4" fill="none" stroke="${K}" stroke-width=".55"/><circle cx="31" cy="24" r="12.4" fill="none" stroke="${K}" stroke-width=".55"/></svg>`;

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
const GO = [
  ['briefcase', 'Открытые задания', 'Берите заказ сразу'],
  ['users', 'Каталог авторов', 'Авторы по теме и городу'],
  ['user-round-pen', 'Создать анкету автора', 'Один раз — и берите заказы'],
];
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
 <div class="price">${price.replace(' ', '&nbsp;')}<small>/ мес</small></div>
 <ul>${items.map(i => `<li>${ic('check', 22, 1.8)}<span>${i}</span></li>`).join('')}</ul>
 <a class="btn ${dark ? (hot ? 'b-l' : 'b-ol') : (hot ? 'b-l' : 'b-d')}">${price === '0 ₽' ? 'Начать бесплатно' : 'Выбрать ' + name}</a></div>`;

module.exports = () => `<!doctype html><html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=1440">
<title>Охват — реклама у авторов видео с оплатой за результат</title>
<link rel="stylesheet" href="fonts/roboto-flex.css"><style>
:root{--dark:${D};--dark-card:${DC};--on-dark:${TL};--btn-light:${BL};--bg:${BG};--card:${C};--ink:${K};--gray:${G};--emerald:${E};--wrap:${T.wrap}px}
*{box-sizing:border-box;margin:0;padding:0}
body{width:1440px;font-family:'Roboto Flex',sans-serif;font-size:18px;line-height:1.5;color:var(--ink);background:var(--bg);-webkit-font-smoothing:antialiased;text-rendering:geometricPrecision}
.w{width:var(--wrap);margin:0 auto;position:relative}
a{color:inherit;text-decoration:none}
ul{list-style:none}
.ic{flex:none;display:block}
h1,h2{font-variation-settings:"opsz" 120,"wdth" 114;font-weight:600}
h2{font-size:52px;line-height:1.04;letter-spacing:-.03em}
h3{font-variation-settings:"wdth" 106;font-weight:600}
.lead{font-size:20px;line-height:1.5;color:var(--gray);margin-top:20px}
.on-dark .lead{color:${lA(.6)}}
.sheet{border-radius:${T.r.sheet}px ${T.r.sheet}px 0 0;margin-top:-${T.r.sheet}px;position:relative;z-index:1}
.on-dark{background:${D};color:${TL}}

/* кнопки-пилюли */
.btn{display:inline-flex;align-items:center;justify-content:center;gap:10px;height:60px;padding:0 32px;border-radius:999px;font-size:18px;font-weight:600;font-variation-settings:"wdth" 106;white-space:nowrap}
.b-l{background:${BL};color:${K}}.b-d{background:${D};color:${TL}}
.b-ol{box-shadow:inset 0 0 0 1.5px ${lA(.26)};color:${TL}}.b-od{box-shadow:inset 0 0 0 1.5px ${kA(.18)};color:${K}}

/* шапка и первый экран */
.logo{display:flex;align-items:center;gap:10px}.logo b{font-size:30px;font-weight:700;letter-spacing:-.045em;font-variation-settings:"wdth" 114;line-height:1}
.nav{position:absolute;top:-128px;left:0;right:0;height:48px;display:grid;grid-template-columns:1fr auto 1fr;align-items:center}
.nav ul{display:flex;gap:36px;font-size:18px;color:${lA(.66)}}
.nav .r{display:flex;align-items:center;justify-content:flex-end;gap:24px;font-size:18px;color:${lA(.66)}}
.nav .btn{height:48px;padding:0 24px;font-size:17px}
.hero{padding:152px 0 ${112 + T.r.sheet}px}
.hero h1{text-align:center;font-size:100px;line-height:1;letter-spacing:-.04em}
.hero h1 span{color:${lA(.5)}}
.hero .sub{text-align:center;font-size:24px;line-height:1.45;font-weight:500;letter-spacing:-.015em;color:${lA(.6)};margin:32px auto 0;max-width:820px;font-variation-settings:"wdth" 106}
.hero .cta{display:flex;gap:12px;justify-content:center;margin-top:44px}
.pics{display:grid;grid-template-columns:repeat(4,1fr);gap:16px;margin-top:96px}
.pic{position:relative;height:400px;border-radius:${T.r.photo}px;overflow:hidden;background:${DC}}
.pic img{width:100%;height:100%;object-fit:cover;display:block}
.pic span{position:absolute;left:16px;bottom:16px;display:inline-flex;align-items:center;gap:8px;height:44px;padding:0 18px 0 14px;border-radius:999px;background:rgba(27,27,26,.62);-webkit-backdrop-filter:blur(14px);backdrop-filter:blur(14px);color:${TL};font-size:18px;font-weight:500}

/* как это работает */
.how{background:${BG};padding-top:112px}
.steps{display:grid;grid-template-columns:repeat(4,1fr);gap:16px;margin-top:56px}
.st{background:${C};border-radius:${T.r.tile}px;padding:32px 30px 32px;display:flex;flex-direction:column;min-height:400px}
.st .top{display:flex;justify-content:space-between;align-items:center}
.st i{width:60px;height:60px;border-radius:50%;background:${BG};display:flex;align-items:center;justify-content:center}
.st .nx{color:${kA(.3)}}
.st h3{font-size:26px;line-height:1.2;letter-spacing:-.02em;margin-top:32px}
.st p{font-size:18px;color:${G};margin-top:10px}
.chips{margin-top:auto;padding-top:24px;display:flex;flex-wrap:wrap;gap:8px}
.chip{height:40px;padding:0 16px;border-radius:999px;box-shadow:inset 0 0 0 1px ${kA(.14)};display:inline-flex;align-items:center;font-size:18px;font-weight:500}
.chip-ok{background:${E};color:${TL};box-shadow:none;gap:8px;padding-left:12px}

/* открытые задания */
.open{background:${BG};padding-top:20px}
.op{border-radius:${T.r.panel}px;padding:60px;display:grid;grid-template-columns:520px 1fr;gap:64px;margin-top:0}
.op-pic{border-radius:${T.r.photo}px;overflow:hidden;min-height:580px}.op-pic img{width:100%;height:100%;object-fit:cover;display:block}
.op h2{font-size:48px}
.op-l{display:flex;flex-direction:column}
.kinds{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin:36px 0 24px}
.kd{border-radius:${T.r.card}px;box-shadow:inset 0 0 0 1.5px ${lA(.12)};padding:26px 24px 28px}
.kd i{width:52px;height:52px;border-radius:50%;background:${DC};display:flex;align-items:center;justify-content:center}
.kd b{display:block;font-size:24px;font-weight:600;letter-spacing:-.02em;margin-top:22px;font-variation-settings:"wdth" 110}
.kd span{display:block;font-size:18px;color:${lA(.6)};margin-top:6px}
.ex{margin-top:auto;background:${DC};border-radius:${T.r.card}px;padding:26px 28px;display:flex;align-items:center;justify-content:space-between;gap:20px}
.ex .tag{display:inline-flex;align-items:center;gap:8px;font-size:18px;color:${lA(.6)}}
.ex b{display:block;font-size:24px;font-weight:600;letter-spacing:-.02em;margin-top:6px;font-variation-settings:"wdth" 110}
.ex .money{display:block;font-size:20px;margin-top:4px;color:${lA(.86)}}

/* тарифы */
.biz{background:${BG};padding:140px 0 ${156 + T.r.sheet}px}
.plans{display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin-top:56px}
.plan{background:${C};border-radius:${T.r.tile}px;padding:40px;display:flex;flex-direction:column}
.pl-h{display:flex;justify-content:space-between;align-items:center;min-height:40px}
.plan h3{font-size:30px;letter-spacing:-.02em;font-variation-settings:"opsz" 120,"wdth" 114}
.rec{height:36px;padding:0 16px;border-radius:999px;background:${E};color:${TL};display:inline-flex;align-items:center;font-size:18px;font-weight:500}
.pl-s{font-size:18px;color:${G};margin-top:8px}
.price small{font-size:20px;font-weight:400;letter-spacing:0;margin-left:10px;opacity:.6;font-variation-settings:normal}
.price{font-size:48px;line-height:1;font-weight:600;letter-spacing:-.03em;margin-top:32px;padding-bottom:32px;border-bottom:1px solid ${kA(.1)};font-variation-settings:"opsz" 120,"wdth" 114}
.plan ul{margin:28px 0 36px;display:flex;flex-direction:column;gap:16px}
.plan li{display:flex;gap:12px;font-size:18px;line-height:1.4}
.plan li .ic{color:${E};margin-top:1px}
.plan .btn{margin-top:auto;width:100%}
.plan-on{background:${D};color:${TL}}.plan-on .pl-s{color:${lA(.6)}}.plan-on .price{border-color:${lA(.12)}}.plan-on li .ic{color:${TL}}
.aut{padding:120px 0 ${156 + T.r.sheet}px}
.aut .plan{background:${DC}}
.aut .pl-s{color:${lA(.6)}}.aut .price{border-color:${lA(.1)}}.aut li .ic{color:${lA(.8)}}
.aut .plan-on{background:#30302D}

/* категории, переходы, вопросы */
.rest{background:${BG};padding:120px 0 164px}
.cgrid{display:grid;grid-template-columns:repeat(4,1fr);gap:12px;margin-top:56px}
.cat{height:80px;border-radius:999px;background:${C};display:flex;align-items:center;gap:16px;padding:0 28px 0 14px;font-size:21px;font-weight:600;letter-spacing:-.015em;font-variation-settings:"wdth" 106}
.cat i{width:52px;height:52px;border-radius:50%;background:${BG};display:inline-flex;align-items:center;justify-content:center;flex:none}
.rest .more{margin-top:40px}
.go{margin-top:140px}
.tiles{display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin-top:56px}
.tile{height:300px;border-radius:${T.r.tile}px;background:${C};padding:40px;display:flex;flex-direction:column}
.tile .top{display:flex;justify-content:space-between;align-items:flex-start}
.tile .ico{width:64px;height:64px;border-radius:50%;background:${BG};display:flex;align-items:center;justify-content:center}
.ar{width:56px;height:56px;border-radius:50%;display:inline-flex;align-items:center;justify-content:center;background:${D};color:${TL};flex:none}
.tile h3{margin-top:auto;font-size:36px;line-height:1.05;letter-spacing:-.03em;font-variation-settings:"opsz" 120,"wdth" 114}
.tile p{font-size:20px;color:${G};margin-top:12px}
.faq{margin-top:140px;border-radius:${T.r.panel}px;background:${C};padding:60px;display:grid;grid-template-columns:361px 1fr;gap:64px}
.faq .side{display:flex;flex-direction:column;align-items:flex-start}
.faq h2{font-size:48px}.faq .btn{margin-top:auto}
.qa{border-top:1px solid ${kA(.1)}}
.q{border-bottom:1px solid ${kA(.1)};padding:28px 0}
.q .h{display:flex;justify-content:space-between;align-items:center;gap:24px;font-size:22px;font-weight:600;letter-spacing:-.015em;font-variation-settings:"wdth" 106}
.q .pm{width:44px;height:44px;border-radius:50%;background:${BG};display:flex;align-items:center;justify-content:center;flex:none}
.q-on .pm{background:${D};color:${TL}}
.q p{font-size:18px;color:${G};margin-top:12px;max-width:560px}
.start{margin-top:16px;border-radius:${T.r.panel}px;background:${C};padding:80px 60px;display:flex;justify-content:space-between;align-items:center;min-height:480px}
.start h2{font-size:68px;line-height:1;letter-spacing:-.035em;max-width:620px}
.start .lead{font-size:22px;margin-top:24px}
.start .cta{display:flex;gap:12px;margin-top:44px}
.start .mk{margin-right:20px}

/* подвал */
.foot{padding:96px 0 40px}
.foot .top{display:grid;grid-template-columns:317px repeat(4,1fr);gap:40px}
.foot .about p{font-size:16px;line-height:1.5;color:${lA(.6)};margin-top:24px}
.foot .about .mail{display:inline-block;margin-top:24px;font-size:16px;border-bottom:1px solid ${lA(.28)};padding-bottom:4px}
.foot h4{font-size:14px;font-weight:500;color:${lA(.55)};margin-bottom:18px;font-variation-settings:"wdth" 106}
.foot .col a{display:block;font-size:16px;margin-bottom:12px}
.foot .bot{display:flex;justify-content:space-between;align-items:center;margin-top:88px;padding-top:28px;border-top:1px solid ${lA(.1)};font-size:14px;color:${lA(.55)}}
.foot .bot b{color:${TL};font-weight:600}
</style></head><body>

<section class="hero on-dark"><div class="w">
 <div class="nav">${logo(TL)}<ul>${NAV.map(n => `<li>${n}</li>`).join('')}</ul>
  <div class="r"><span>Войти</span><a class="btn b-l">Разместить задание</a></div></div>
 <h1>Реклама у авторов видео<br>— оплата за результат</h1>
 <p class="sub">Разместите задание с условиями.<br>Автор выполнит — сервис проверит и переведёт оплату.</p>
 <div class="cta"><a class="btn b-l">Разместить задание</a><a class="btn b-ol">Найти заказы</a></div>
 <div class="pics">${PHOTOS.map(([f, i, t]) => `<div class="pic"><img src="../assets/${f}.jpg" alt=""><span>${ic(i, 20, 1.7)}${t}</span></div>`).join('')}</div>
</div></section>

<section class="how sheet"><div class="w">
 <h2>Как это работает<br>для бизнеса</h2>
 <p class="lead">Четыре шага — от тарифа до оплаты.</p>
 <div class="steps">${STEPS.map(([i, t, d, chips], n) => `<div class="st"><div class="top"><i>${ic(i, 26, 1.6)}</i>${n < 3 ? `<span class="nx">${ic('arrow-right', 24, 1.6)}</span>` : ''}</div>
  <h3>${t}</h3><p>${d}</p><div class="chips">${chips ? chips.map(c => `<span class="chip">${c}</span>`).join('') : `<span class="chip chip-ok">${ic('check', 20, 2)}Оплачено автору</span>`}</div></div>`).join('')}</div>
</div></section>

<section class="open"><div class="w">
 <div class="op on-dark"><div class="op-pic"><img src="../assets/creator.jpg" alt=""></div>
  <div class="op-l"><h2>Два вида заданий</h2>
   <p class="lead">Деньги хранятся в безопасной сделке и&nbsp;уходят автору автоматически после проверки.</p>
   <div class="kinds">${KINDS.map(([i, t, d]) => `<div class="kd"><i>${ic(i, 24, 1.6)}</i><b>${t}</b><span>${d}</span></div>`).join('')}</div>
   <div class="ex"><div><span class="tag">${ic('zap', 18, 1.7)}Кофейня · Казань</span><b>10 000 просмотров</b><span class="money">3 000 ₽ за выполнение</span></div><a class="btn b-l">Взять задание</a></div>
  </div></div>
</div></section>

<section class="biz"><div class="w">
 <h2>Тарифы для бизнеса</h2>
 <p class="lead">Платите за размещение, а автору — только за результат.</p>
 <div class="plans">${BIZ.map(p => plan(p, 0)).join('')}</div>
</div></section>

<section class="aut on-dark sheet"><div class="w">
 <h2>Тарифы для авторов</h2>
 <p class="lead">Больше откликов — больше заказов.</p>
 <div class="plans">${AUT.map(p => plan(p, 1)).join('')}</div>
</div></section>

<section class="rest sheet"><div class="w">
 <h2>Задания в любой теме</h2>
 <p class="lead">Выберите свою — и смотрите заказы по ней.</p>
 <div class="cgrid">${CATS.map(([i, t]) => `<a class="cat"><i>${ic(i, 24, 1.6)}</i>${t}</a>`).join('')}</div>
 <a class="btn b-d more">Все категории ${ic('arrow-right', 20, 1.8)}</a>

 <div class="go"><h2>С чего начать</h2>
  <div class="tiles">${GO.map(([i, t, d]) => `<a class="tile"><div class="top"><span class="ico">${ic(i, 28, 1.5)}</span>${arrow()}</div><h3>${t}</h3><p>${d}</p></a>`).join('')}</div></div>

 <div class="faq"><div class="side"><h2>Частые вопросы</h2><p class="lead">Не нашли ответ — напишите нам.</p><a class="btn b-od">Все вопросы</a></div>
  <div class="qa">${FAQ.map(([q, a]) => `<div class="q ${a ? 'q-on' : ''}"><div class="h">${q}<span class="pm">${ic(a ? 'minus' : 'plus', 20, 1.8)}</span></div>${a ? `<p>${a}</p>` : ''}</div>`).join('')}</div></div>
 <div class="start"><div><h2>Разместите первое задание</h2>
   <p class="lead">Начните бесплатно — авторам платите только за&nbsp;результат.</p>
   <div class="cta"><a class="btn b-d">Разместить задание</a><a class="btn b-od">Я автор</a></div></div>
  <div class="mk">${bigMark(400)}</div></div>
</div></section>

<footer class="foot on-dark sheet"><div class="w">
 <div class="top"><div class="about">${logo(TL)}<p>Реклама у авторов видео с оплатой за результат: бизнес размещает задания, авторы выполняют, сервис проверяет.</p><a class="mail">[УТОЧНИТЬ: почта поддержки]</a></div>
  <div class="col"><h4>Бизнесу</h4><a>Разместить задание</a><a>Тарифы</a><a>Каталог авторов</a><a>Как это работает</a></div>
  <div class="col"><h4>Авторам</h4><a>Открытые задания</a><a>Создать анкету</a><a>Тарифы для авторов</a></div>
  <div class="col"><h4>Темы</h4><a>Еда и кафе</a><a>Красота и салоны</a><a>Спорт и фитнес</a><a>Цветы и подарки</a><a>Все категории</a></div>
  <div class="col"><h4>Документы</h4><a>Пользовательское соглашение</a><a>Политика конфиденциальности</a><a>Согласие на обработку данных</a><a>Правила площадки</a></div></div>
 <div class="bot"><span>© 2026 <b>Охват</b>. Все права защищены.</span><span>[УТОЧНИТЬ: юрлицо и ИНН]</span></div>
</div></footer>
</body></html>`;
