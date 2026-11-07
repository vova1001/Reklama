// «Охват» — главная, финал (задача 11). Инструмент отрисовки эскиза, не код сайта.
// Стилистика — как у референса (полотно): чёрный первый экран с шапкой → белый лист со скруглением 44,
// панели r40 светло-серые и чёрные, контейнер 1176, Roboto Flex (wdth 114 в заголовках), пилюли 60 px.
// Без телефонов и экранов интерфейса. Содержание — наше (handoff/09 «Круг 2», handoff/11).
// Цвета: чёрный #1A1A1A, светлый #F5F5F3, белый, изумруд #12805C — точечно. Текст вне подвала ≥ 18 px.
const fs = require('fs'), path = require('path');

// ===== токены =====
const T = {
  ink: '#1A1A1A', paper: '#FFFFFF', mist: '#F5F5F3', emerald: '#12805C',
  gray: '#5E5E5E', line: '#E3E3E0',
  r: { sheet: 44, panel: 40, tile: 32, card: 28 },
  wrap: 1176,
};
const { ink: K, paper: W, mist: L, emerald: E } = T;
const kA = a => `rgba(26,26,26,${a})`, lA = a => `rgba(245,245,243,${a})`;

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

// Большой знак для финала: те же пропорции, что у логотипа
const bigMark = (w) => {
  const h = Math.round(w * 26 / 40);
  return `<svg width="${w}" height="${h}" viewBox="2 11 44 26" aria-hidden="true">
<path d="M24 13.765A12.4 12.4 0 0 1 24 34.235A12.4 12.4 0 0 1 24 13.765Z" fill="${E}"/>
<circle cx="17" cy="24" r="12.4" fill="none" stroke="${K}" stroke-width=".55"/><circle cx="31" cy="24" r="12.4" fill="none" stroke="${K}" stroke-width=".55"/></svg>`;
};

// Схема «две стороны» для панелей: своя сторона залита, пересечение — изумруд
function venn(side, dark) {
  const r = 88, c1 = 88, c2 = 316 - 88, cy = 88, ring = dark ? lA(.34) : kA(.22);
  const fillL = side === 'b' ? (dark ? L : K) : 'none', fillR = side === 'a' ? (dark ? L : K) : 'none';
  const txt = (fill) => fill === 'none' ? (dark ? L : K) : (dark ? K : L);
  return `<svg class="venn" width="316" height="176" viewBox="0 0 316 176" aria-hidden="true">
<defs><clipPath id="cl-${side}"><circle cx="${c1}" cy="${cy}" r="${r}"/></clipPath></defs>
<circle cx="${c1}" cy="${cy}" r="${r - .75}" fill="${fillL}" stroke="${fillL === 'none' ? ring : 'none'}" stroke-width="1.5"/>
<circle cx="${c2}" cy="${cy}" r="${r - .75}" fill="${fillR}" stroke="${fillR === 'none' ? ring : 'none'}" stroke-width="1.5"/>
<circle cx="${c2}" cy="${cy}" r="${r}" fill="${E}" clip-path="url(#cl-${side})"/>
<text x="${c1 - 22}" y="${cy + 6}" text-anchor="middle" fill="${txt(fillL)}">Бизнес</text>
<text x="${c2 + 22}" y="${cy + 6}" text-anchor="middle" fill="${txt(fillR)}">Автор</text></svg>`;
}

// ===== данные =====
const NAV = ['Бизнесу', 'Авторам', 'Категории', 'Вопросы'];

// Лента первого экрана: задание крупно · тема-метка · формат и город. Без компаний, цен и сроков.
const ROW1 = [
  ['sparkles', 'Красота', 'Обзор крема для рук', 'Reels · Москва'],
  ['coffee', 'Еда и кафе', 'Открытие второй кофейни', 'VK Клипы · Казань'],
  ['dumbbell', 'Спорт', 'Тест беговых кроссовок', 'Shorts · онлайн', 1],
  ['paw-print', 'Животные', 'Новый корм для щенков', 'TikTok · Пермь'],
  ['baby', 'Дети', 'Конструктор для&nbsp;школьников', 'Reels · онлайн'],
];
const ROW2 = [
  ['house', 'Дом и ремонт', 'Ремонт ванной под ключ', 'Reels · Самара'],
  ['graduation-cap', 'Образование', 'Курс английского для&nbsp;взрослых', 'Интеграция · онлайн'],
  ['car', 'Авто', 'Шиномонтаж рядом с&nbsp;домом', 'VK Клипы · Екатеринбург'],
  ['wallet', 'Финансы', 'Приложение для&nbsp;учёта&nbsp;трат', 'Shorts · онлайн'],
  ['shirt', 'Мода', 'Новая коллекция платьев', 'TikTok · Москва'],
];

const BIZ = [
  ['file-text', 'Задание как вакансия', 'Всё о заказе на одной странице'],
  ['message-circle', 'Отклики сами', 'Авторы пишут вам первыми'],
  ['eye', 'Видно, кто смотрит', 'Зрители автора — в отклике'],
  ['wallet', 'Любой бюджет', 'Сумму в задании ставите вы'],
  ['columns-3', 'Сравнение в одном месте', 'Идеи, примеры и цены — рядом'],
  ['shield-check', 'Безопасная сделка', 'Всё фиксируется в сервисе'],
];
const AUT = [
  ['search', 'Заказы по теме', 'Лента по вашей теме и городу'],
  ['list-checks', 'Условия до отклика', 'Формат, срок и бюджет — сразу'],
  ['lightbulb', 'Отклик с идеей', 'Предложите свой ролик и цену'],
  ['user-round-check', 'Малый канал — на равных', 'Важны зрители, а не подписчики'],
  ['star', 'Рейтинг по сделкам', 'Отзывы после каждого заказа'],
  ['id-card', 'Анкета один раз', 'Дальше — отклик в пару нажатий'],
];

const CATS = [
  ['sparkles', 'Красота'], ['coffee', 'Еда и кафе'], ['dumbbell', 'Спорт'], ['plane', 'Путешествия'],
  ['paw-print', 'Животные'], ['baby', 'Дети'], ['shirt', 'Мода'], ['smartphone', 'Техника'],
  ['car', 'Авто'], ['house', 'Дом и ремонт'], ['graduation-cap', 'Образование'], ['heart-pulse', 'Здоровье'],
  ['gamepad-2', 'Игры'], ['wallet', 'Финансы'], ['store', 'Местный бизнес'], ['globe', 'Онлайн-сервисы'],
];

const STEPS = [
  ['file-text', 'Задание', 'Бизнес описывает, что&nbsp;нужно'],
  ['message-circle', 'Отклики', 'Авторы предлагают идею и&nbsp;цену'],
  ['messages-square', 'Выбор и чат', 'Договариваетесь в&nbsp;одном&nbsp;окне'],
  ['play', 'Ролик и отзыв', 'Ролик вышел — стороны ставят оценку'],
];

const GO = [
  ['briefcase', 'Свежие задания', 'Новые заказы от бизнеса'],
  ['users', 'Каталог авторов', 'Авторы по теме и городу'],
  ['user-round-pen', 'Создать анкету автора', 'Один раз — и берите заказы'],
];

const FAQ = [
  ['Кто откликается на задания?', 'Авторы видео, чья тема совпадает с вашим заданием. Вы видите отклики и выбираете сами.'],
  ['Как выбрать подходящего автора?'],
  ['Можно найти автора в своём городе?'],
  ['Я автор. С чего начать?'],
];

// ===== части =====
const arrow = (cls = '') => `<span class="ar ${cls}">${ic('arrow-up-right', 22, 1.8)}</span>`;
const card = ([i, tag, t, meta, hot]) => `<div class="tk ${hot ? 'tk-on' : ''}">
 <span class="tag">${ic(i, 18, 1.7)}${tag}</span>
 <b>${t}</b>
 <div class="tk-f"><span>${meta}</span>${arrow()}</div></div>`;
const feats = list => `<div class="feats">${list.map(([i, t, d]) => `<div class="ft">${ic(i, 28, 1.5)}<h3>${t}</h3><p>${d}</p></div>`).join('')}</div>`;

module.exports = () => `<!doctype html><html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=1440">
<title>Охват — реклама у авторов видео</title>
<link rel="stylesheet" href="fonts/roboto-flex.css"><style>
:root{--ink:${K};--paper:${W};--mist:${L};--emerald:${E};--gray:${T.gray};--line:${T.line};--wrap:${T.wrap}px}
*{box-sizing:border-box;margin:0;padding:0}
body{width:1440px;font-family:'Roboto Flex',sans-serif;font-size:18px;line-height:1.5;color:var(--ink);background:var(--paper);-webkit-font-smoothing:antialiased;text-rendering:geometricPrecision}
.w{width:var(--wrap);margin:0 auto;position:relative}
a{color:inherit;text-decoration:none}
.ic{flex:none;display:block}
h1,h2,.hd{font-variation-settings:"opsz" 120,"wdth" 114;font-weight:600}
h2{font-size:52px;line-height:1.04;letter-spacing:-.03em}
h3{font-variation-settings:"wdth" 106;font-weight:600}
.lead{font-size:20px;line-height:1.5;color:var(--gray);margin-top:20px}
.sheet{border-radius:${T.r.sheet}px ${T.r.sheet}px 0 0;margin-top:-${T.r.sheet}px;position:relative;z-index:1}

/* кнопки-пилюли */
.btn{display:inline-flex;align-items:center;justify-content:center;gap:10px;height:60px;padding:0 32px;border-radius:999px;font-size:18px;font-weight:600;font-variation-settings:"wdth" 106;white-space:nowrap}
.btn .ic{margin-right:-6px}
.b-w{background:${W};color:${K}}.b-k{background:${K};color:${W}}
.b-ow{box-shadow:inset 0 0 0 1.5px ${lA(.3)};color:${L}}.b-ok{box-shadow:inset 0 0 0 1.5px ${kA(.18)};color:${K};background:${W}}

/* шапка */
.logo{display:flex;align-items:center;gap:10px}.logo b{font-size:30px;font-weight:700;letter-spacing:-.045em;font-variation-settings:"wdth" 114;line-height:1}
.nav{position:absolute;top:-128px;left:0;right:0;height:48px;display:grid;grid-template-columns:1fr auto 1fr;align-items:center}
.nav ul{display:flex;gap:36px;list-style:none;font-size:18px;color:${lA(.7)}}
.nav .r{display:flex;align-items:center;justify-content:flex-end;gap:24px;font-size:18px;color:${lA(.7)}}
.nav .btn{height:48px;padding:0 24px;font-size:17px}

/* первый экран */
.hero{background:${K};color:${L};padding:152px 0 ${120 + T.r.sheet}px;position:relative;overflow:hidden}
.hero h1{text-align:center;font-size:116px;line-height:.98;letter-spacing:-.04em}
.hero h1 span{color:${lA(.42)}}
.hero .sub{text-align:center;font-size:24px;line-height:1.45;font-weight:500;letter-spacing:-.02em;color:${lA(.62)};margin-top:32px;font-variation-settings:"wdth" 106}
.hero .cta{display:flex;gap:12px;justify-content:center;margin-top:44px}
.board{margin-top:100px;display:flex;flex-direction:column;gap:16px;-webkit-mask-image:linear-gradient(90deg,transparent 0,#000 200px,#000 1240px,transparent 1440px)}
.row{display:flex;gap:16px;width:max-content}
.tk{width:352px;height:228px;border-radius:${T.r.card}px;background:${lA(.055)};box-shadow:inset 0 0 0 1px ${lA(.08)};padding:26px 26px 24px;display:flex;flex-direction:column}
.tag{align-self:flex-start;display:inline-flex;align-items:center;gap:8px;height:38px;padding:0 16px 0 13px;border-radius:999px;box-shadow:inset 0 0 0 1px ${lA(.16)};font-size:18px;font-weight:500;color:${lA(.86)}}
.tk b{display:block;font-size:28px;line-height:1.1;font-weight:600;letter-spacing:-.025em;margin-top:20px;font-variation-settings:"opsz" 60,"wdth" 110}
.tk-f{margin-top:auto;display:flex;align-items:center;justify-content:space-between;font-size:18px;color:${lA(.56)}}
.ar{width:48px;height:48px;border-radius:50%;display:inline-flex;align-items:center;justify-content:center;background:${lA(.1)};color:${L};flex:none}
.tk-on{background:${L};color:${K};box-shadow:none}
.tk-on .tag{box-shadow:inset 0 0 0 1px ${kA(.14)};color:${K}}
.tk-on .tk-f{color:${T.gray}}.tk-on .ar{background:${K};color:${L}}

/* кому это нужно */
.who{background:${W};padding-top:112px}
.panel{border-radius:${T.r.panel}px;padding:60px;display:grid;grid-template-columns:361px 1fr;gap:64px;position:relative}
.p-light{background:${L};margin-top:64px}
.p-dark{background:${K};color:${L};margin-top:20px}
.panel .side{display:flex;flex-direction:column;align-items:flex-start}
.panel h2{font-size:48px;line-height:1.04}
.panel .lead{margin-top:20px}
.p-dark .lead{color:${lA(.62)}}
.panel .btn{margin-top:36px}
.venn{margin-top:auto;padding-top:56px;display:block;box-sizing:content-box}
.venn text{font-family:'Roboto Flex';font-size:19px;font-weight:600;font-variation-settings:"wdth" 106;letter-spacing:-.01em}
.feats{display:grid;grid-template-columns:1fr 1fr;column-gap:48px;align-content:start}
.ft{padding:30px 0 32px;border-bottom:1px solid ${kA(.1)}}
.ft:nth-child(-n+2){padding-top:2px}.ft:nth-last-child(-n+2){border-bottom:0;padding-bottom:0}
.p-dark .ft{border-color:${lA(.12)}}
.ft h3{font-size:22px;line-height:1.3;letter-spacing:-.015em;margin-top:18px}
.ft p{font-size:18px;line-height:1.5;color:${T.gray};margin-top:6px}
.p-dark .ft p{color:${lA(.62)}}

/* категории */
.cats{padding-top:140px}
.cgrid{display:grid;grid-template-columns:repeat(4,1fr);gap:12px;margin-top:56px}
.cat{height:80px;border-radius:999px;background:${L};display:flex;align-items:center;gap:16px;padding:0 28px 0 14px;font-size:21px;font-weight:600;letter-spacing:-.015em;font-variation-settings:"wdth" 106}
.cat i{width:52px;height:52px;border-radius:50%;background:${W};display:inline-flex;align-items:center;justify-content:center;flex:none}
.cats .btn{margin-top:40px}

/* как это работает */
.how{padding:140px 0 ${164 + T.r.sheet}px}
.steps{display:grid;grid-template-columns:repeat(4,1fr);margin-top:56px;border-radius:${T.r.panel}px;background:${L};overflow:hidden}
.st{padding:36px 32px 40px;border-left:1px solid ${T.line};position:relative}.st:first-child{border-left:0}
.st i{width:60px;height:60px;border-radius:50%;background:${W};display:flex;align-items:center;justify-content:center}
.st .nx{position:absolute;right:32px;top:54px;color:${kA(.32)}}
.st h3{font-size:26px;line-height:1.2;letter-spacing:-.02em;margin-top:32px}
.st p{font-size:18px;line-height:1.5;color:${T.gray};margin-top:10px}

/* переходы — чёрная полоса */
.go{background:${K};color:${L};padding:120px 0 ${156 + T.r.sheet}px}
.go .lead{color:${lA(.62)}}
.tiles{display:grid;grid-template-columns:repeat(3,1fr);gap:20px;margin-top:56px}
.tile{height:340px;border-radius:${T.r.tile}px;background:${lA(.055)};box-shadow:inset 0 0 0 1px ${lA(.08)};padding:40px;display:flex;flex-direction:column}
.tile .top{display:flex;justify-content:space-between;align-items:flex-start}
.tile .ico{width:64px;height:64px;border-radius:50%;box-shadow:inset 0 0 0 1.5px ${lA(.18)};display:flex;align-items:center;justify-content:center}
.tile .ar{width:60px;height:60px;background:${L};color:${K}}
.tile h3{margin-top:auto;font-size:38px;line-height:1.04;letter-spacing:-.03em;font-variation-settings:"opsz" 120,"wdth" 114;display:flex;align-items:center;gap:14px}
.tile p{font-size:20px;color:${lA(.62)};margin-top:14px}
.tile-on{background:${L};color:${K};box-shadow:none}.tile-on p{color:${T.gray}}.tile-on .ico{box-shadow:inset 0 0 0 1.5px ${kA(.16)}}.tile-on .ar{background:${K};color:${L}}

/* вопросы и старт */
.end{background:${W};padding:120px 0 164px}
.faq{border-radius:${T.r.panel}px;background:${L};padding:60px;display:grid;grid-template-columns:361px 1fr;gap:64px}
.faq .side{display:flex;flex-direction:column;align-items:flex-start}
.faq h2{font-size:48px}
.faq .btn{margin-top:auto}
.qa{border-top:1px solid ${kA(.12)}}
.q{border-bottom:1px solid ${kA(.12)};padding:28px 0}
.q .h{display:flex;justify-content:space-between;align-items:center;gap:24px;font-size:22px;font-weight:600;letter-spacing:-.015em;font-variation-settings:"wdth" 106}
.q .pm{width:44px;height:44px;border-radius:50%;background:${W};display:flex;align-items:center;justify-content:center;flex:none}
.q-on .pm{background:${K};color:${L}}
.q p{font-size:18px;color:${T.gray};margin-top:12px;max-width:560px}
.start{margin-top:20px;border-radius:${T.r.panel}px;background:${L};padding:80px 60px;display:flex;justify-content:space-between;align-items:center;min-height:500px;overflow:hidden}
.start h2{font-size:68px;line-height:1.0;letter-spacing:-.035em;max-width:600px}
.start .lead{font-size:22px;margin-top:24px}
.start .cta{display:flex;gap:12px;margin-top:44px}
.start .mk{margin-right:20px}

/* подвал */
.foot{background:${K};color:${L};padding:96px 0 40px}
.foot .top{display:grid;grid-template-columns:317px repeat(4,1fr);gap:40px}
.foot .about p{font-size:16px;line-height:1.5;color:${lA(.62)};margin-top:24px}
.foot .about .mail{display:inline-block;margin-top:24px;font-size:16px;border-bottom:1px solid ${lA(.3)};padding-bottom:4px}
.foot h4{font-size:14px;font-weight:500;color:${lA(.56)};margin-bottom:18px;font-variation-settings:"wdth" 106}
.foot .col a{display:block;font-size:16px;margin-bottom:12px}
.foot .bot{display:flex;justify-content:space-between;align-items:center;margin-top:88px;padding-top:28px;border-top:1px solid ${lA(.12)};font-size:14px;color:${lA(.56)}}
.foot .bot b{color:${L};font-weight:600}
</style></head><body>

<section class="hero"><div class="w">
 <div class="nav">${logo(L)}<ul>${NAV.map(n => `<li>${n}</li>`).join('')}</ul>
  <div class="r"><span>Войти</span><a class="btn b-w">Разместить задание</a></div></div>
 <h1>Реклама<br>у авторов видео</h1>
 <p class="sub">Бизнес размещает задания. Авторы берут заказы по своей теме.</p>
 <div class="cta"><a class="btn b-w">Разместить задание</a><a class="btn b-ow">Найти заказы</a></div>
</div>
<div class="board">
 <div class="row" style="margin-left:-112px">${ROW1.map(card).join('')}</div>
 <div class="row" style="margin-left:-296px">${ROW2.map(card).join('')}</div>
</div></section>

<section class="who sheet"><div class="w">
 <h2>Кому это нужно</h2>
 <p class="lead">Две стороны — одна площадка.</p>
 <div class="panel p-light"><div class="side"><h2>Бизнесу</h2>
   <p class="lead">Нужна реклама? Разместите задание&nbsp;— авторы откликнутся сами.</p>
   <a class="btn b-k">Разместить задание</a>${venn('b', 0)}</div>
  ${feats(BIZ)}</div>
 <div class="panel p-dark"><div class="side"><h2>Авторам видео</h2>
   <p class="lead">Снимаете ролики? Берите рекламу по своей теме.</p>
   <a class="btn b-w">Смотреть задания</a>${venn('a', 1)}</div>
  ${feats(AUT)}</div>
</div></section>

<section class="cats"><div class="w">
 <h2>Задания в любой теме</h2>
 <p class="lead">Выберите свою — и смотрите заказы по ней.</p>
 <div class="cgrid">${CATS.map(([i, t]) => `<a class="cat"><i>${ic(i, 24, 1.6)}</i>${t}</a>`).join('')}</div>
 <a class="btn b-k">Все категории ${ic('arrow-right', 20, 1.8)}</a>
</div></section>

<section class="how"><div class="w">
 <h2>Как это работает</h2>
 <div class="steps">${STEPS.map(([i, t, d], n) => `<div class="st"><i>${ic(i, 26, 1.6)}</i>${n < 3 ? `<span class="nx">${ic('arrow-right', 24, 1.6)}</span>` : ''}<h3>${t}</h3><p>${d}</p></div>`).join('')}</div>
</div></section>

<section class="go sheet"><div class="w">
 <h2>С чего начать</h2>
 <p class="lead">Смотрите задания, ищите авторов или расскажите о себе.</p>
 <div class="tiles">${GO.map(([i, t, d], n) => `<a class="tile ${n === 0 ? 'tile-on' : ''}"><div class="top"><span class="ico">${ic(i, 28, 1.5)}</span>${arrow()}</div><h3>${t}</h3><p>${d}</p></a>`).join('')}</div>
</div></section>

<section class="end sheet"><div class="w">
 <div class="faq"><div class="side"><h2>Частые вопросы</h2><p class="lead">Не нашли ответ — напишите нам.</p><a class="btn b-ok">Все вопросы</a></div>
  <div class="qa">${FAQ.map(([q, a], n) => `<div class="q ${a ? 'q-on' : ''}"><div class="h">${q}<span class="pm">${ic(a ? 'minus' : 'plus', 20, 1.8)}</span></div>${a ? `<p>${a}</p>` : ''}</div>`).join('')}</div></div>
 <div class="start"><div><h2>Разместите первое задание</h2>
   <p class="lead">Авторы откликнутся сами.</p>
   <div class="cta"><a class="btn b-k">Разместить задание</a><a class="btn b-ok">Я автор</a></div></div>
  <div class="mk">${bigMark(400)}</div></div>
</div></section>

<footer class="foot sheet"><div class="w">
 <div class="top"><div class="about">${logo(L)}<p>Реклама у авторов видео: бизнес размещает задания, авторы берут заказы по своей теме.</p><a class="mail">[УТОЧНИТЬ: почта поддержки]</a></div>
  <div class="col"><h4>Бизнесу</h4><a>Разместить задание</a><a>Каталог авторов</a><a>Как это работает</a><a>Частые вопросы</a></div>
  <div class="col"><h4>Авторам</h4><a>Свежие задания</a><a>Создать анкету</a><a>Рейтинг авторов</a></div>
  <div class="col"><h4>Темы</h4><a>Красота</a><a>Еда и кафе</a><a>Спорт</a><a>Техника</a><a>Местный бизнес</a><a>Все категории</a></div>
  <div class="col"><h4>Документы</h4><a>Пользовательское соглашение</a><a>Политика конфиденциальности</a><a>Согласие на обработку данных</a><a>Правила площадки</a></div></div>
 <div class="bot"><span>© 2026 <b>Охват</b>. Все права защищены.</span><span>[УТОЧНИТЬ: юрлицо и ИНН]</span></div>
</div></footer>
</body></html>`;
