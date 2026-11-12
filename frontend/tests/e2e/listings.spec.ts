import { test, expect, type Browser, type Page, type TestInfo } from '@playwright/test';
import { jsErrors, login, uniq } from './helpers';
import { botOn, chatId, connectTelegram, tgUser } from './social';

// Задание бизнеса и анкета автора: редактор на весь экран, черновик сохраняется сам, удобная проверка (ошибка после
// ухода с поля, «Опубликовать» ведёт к первому незаполненному, «Осталось: …»), блоки (Enter, Backspace, вставка
// строк, Alt+↑/↓, шаблоны), фото, предпросмотр, автоматическая проверка (MODERATION_MODE=auto — сразу «Опубликовано»),
// лента карточками у другого пользователя: наведение — главное поверх фото, нажатие — страница целиком.
const fixture = (name: string) => new URL(`./fixtures/${name}`, import.meta.url).pathname;
const saved = (page: Page) => expect(page.locator('[data-state]')).toContainText('Сохранено', { timeout: 10_000 });
/** Задание сохранилось на сервере (не только черновик формы). */
async function taskSaved(page: Page) {
  await expect(page).toHaveURL(/\/app\/tasks\/\d+$/, { timeout: 10_000 });
}
const vals = (l: ReturnType<Page['locator']>) => l.evaluateAll((els) => els.map((e) => (e as HTMLInputElement).value));
/** Редактор готов (черновик восстановлен, обработчики на месте). */
const ready = (page: Page) => expect(page.locator('[data-editor][data-ready]')).toHaveCount(1);
const tag = () => `${Date.now().toString(36)}${Math.floor(Math.random() * 1e4)}`;

test.beforeEach(async ({ context }) => {
  await context.route('https://telegram.org/**', (r) => r.abort());
});

const MONTHS_GEN = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря'];
function dateIn(days: number) {
  const d = new Date();
  d.setDate(d.getDate() + days);
  const p = (n: number) => String(n).padStart(2, '0');
  return {
    ru: `${p(d.getDate())}.${p(d.getMonth() + 1)}.${d.getFullYear()}`, iso: `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`,
    label: `${d.getDate()} ${MONTHS_GEN[d.getMonth()]} ${d.getFullYear()}`, month: d.getMonth(),
  };
}

async function csrf(page: Page) {
  return { 'X-CSRF-Token': (await (await page.request.get('/api/auth/config')).json()).csrfToken as string };
}

/** Второй пользователь в своём окне (тот же вид устройства), сразу в нужном режиме. */
async function otherUser(browser: Browser, info: TestInfo, name: string, mode: 'business' | 'creator') {
  const u = info.project.use;
  const ctx = await browser.newContext({ baseURL: u.baseURL, viewport: u.viewport, isMobile: u.isMobile, hasTouch: u.hasTouch, userAgent: u.userAgent, deviceScaleFactor: u.deviceScaleFactor });
  const page = await ctx.newPage();
  await login(page, uniq(info.project.name, name));
  expect((await page.request.patch('/api/me/mode', { headers: await csrf(page), data: { mode } })).status()).toBe(200);
  return page;
}

/** Вставка нескольких строк в поле (как Ctrl+V). */
async function paste(page: Page, selector: string, text: string) {
  await page.locator(selector).evaluate((el, t) => {
    const dt = new DataTransfer();
    dt.setData('text/plain', t);
    el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }));
  }, text);
}

/** Карточка в ленте: на устройствах с наведением — «полотно» поверх фото, на телефоне — строка под фото. */
async function expectTileSummary(page: Page, tile: ReturnType<Page['locator']>, text: RegExp, info: TestInfo) {
  if (info.project.use.hasTouch) {
    await expect(tile.locator('.tile-brief')).toBeVisible();
    await expect(tile.locator('.tile-brief')).toContainText(text);
    await expect(tile.locator('[data-sheet]')).toBeHidden();
  } else {
    const sheet = tile.locator('[data-sheet]');
    await expect(sheet).toHaveCSS('opacity', '0');
    await tile.hover();
    await expect(sheet).toHaveCSS('opacity', '1');
    await expect(sheet).toContainText(text);
    await expect(tile.locator('.tile-brief')).toBeHidden();
  }
}

test('задание: редактор на весь экран, проверка у полей, блоки, фото → сразу опубликовано, видно автору', async ({ page, browser }, info) => {
  test.slow();
  const errors = jsErrors(page);
  const city = `Казань ${tag()}`;
  await login(page, uniq(info.project.name, 'task'));
  await page.goto('/app/tasks/new');
  await ready(page);
  // навигация приложения остаётся; над колонкой — панель формы: ✕, «Правка / Как увидят», сохранение, «Опубликовать»
  await expect(page.locator('.rail:visible, .bn:visible')).toHaveCount(1);
  const bar = page.locator('.ebar');
  await expect(bar.getByRole('link', { name: 'Закрыть' })).toBeVisible();
  await expect(bar.getByRole('tab', { name: 'Как увидят' })).toBeVisible();
  const submit = bar.getByRole('button', { name: 'Опубликовать' });
  await expect(submit).toBeEnabled();
  const left = page.locator('[data-left]');
  await expect(left).toContainText('Осталось: название, сфера, город');

  // «Опубликовать» сразу — к первому незаполненному: подсветка, курсор, текст ошибки как у сервера
  await submit.click();
  await expect(page.locator('[data-err="title"]')).toHaveText('Название — от 5 до 100 символов');
  await expect(page.getByLabel('Название задания')).toBeFocused();
  await expect(page.locator('[data-sec="title"]')).toHaveClass(/is-bad/);
  // «Осталось»: слово ведёт к полю
  await left.getByRole('button', { name: 'сфера' }).click();
  await expect(page.locator('[data-sec="categories"]')).toHaveClass(/is-bad/);

  // первая правка создаёт черновик; ошибка уходит, как только поле заполнено
  await page.getByLabel('Название задания').fill('Сторис о новом осеннем меню');
  await expect(page.locator('[data-err="title"]')).toBeHidden();
  await saved(page);
  await taskSaved(page);
  const id = Number(page.url().split('/').pop());

  // ошибка у поля — после ухода с него
  await page.getByLabel('Город').fill('К');
  await page.getByLabel('Просмотры от').focus();
  await expect(page.locator('[data-err="city"]')).toHaveText('Укажите город или «Вся Россия»');
  await page.getByLabel('Город').fill(city);
  await expect(page.locator('[data-err="city"]')).toBeHidden();

  // разделы формы — с заголовком и подсказкой; подписи и поля крупные
  await expect(page.locator('.grp-h')).toHaveText(['Основное', 'Условия', 'Описание', 'Фото']);
  await expect(page.locator('.grp-d').first()).toHaveCSS('font-size', '15px');
  await expect(page.locator('label[for="t-city"]')).toHaveCSS('font-size', '17px');
  await expect(page.locator('label[for="t-city"]')).toHaveCSS('font-weight', '600');
  expect((await page.getByLabel('Город').boundingBox())!.height).toBeGreaterThanOrEqual(52);
  await expect(page.getByLabel('Город')).toHaveCSS('font-size', '17px');

  // сфер — несколько: отмечены галочкой
  const cats = page.locator('[data-cats] [data-slug]');
  await cats.nth(0).click();
  await cats.nth(1).click();
  await expect(cats.nth(1)).toHaveAttribute('aria-pressed', 'true');
  await expect(cats.nth(1).locator('.ic').first()).toHaveCSS('opacity', '1');
  const slugs = [await cats.nth(0).getAttribute('data-slug'), await cats.nth(1).getAttribute('data-slug')];
  // форматы — до 3, «+ Свой» с подсказками
  const fmt = page.locator('[data-formats]');
  await fmt.getByRole('button', { name: 'Reels' }).click();
  await fmt.getByRole('button', { name: 'Shorts' }).click();
  await fmt.getByRole('button', { name: 'Свой' }).click();
  const fcb = page.getByRole('combobox', { name: 'Свой формат' });
  await fcb.fill('прямой эф');
  await expect(page.getByRole('option', { name: 'Прямой эфир' })).toBeVisible();
  await fcb.press('Enter');
  await expect(fmt.locator('.chip.is-own[aria-pressed="true"]')).toHaveText('Прямой эфир');
  await fmt.getByRole('button', { name: 'TikTok' }).click();
  await expect(page.locator('[data-toast]')).toContainText('Не больше 3 форматов');
  await expect(fmt.getByRole('button', { name: 'TikTok' })).toHaveAttribute('aria-pressed', 'false');
  await page.getByRole('radio', { name: 'С одобрением' }).click();
  await page.getByLabel('Просмотры от').fill('5000');
  await expect(page.getByLabel('Просмотры от')).toHaveValue('5 000');
  await page.getByLabel('Оплата').fill('3000');
  await expect(page.getByLabel('Лайки от')).toHaveCount(0); // условие — только просмотры
  // срок обязателен: пилюли или «+ Свой» — свой календарь (от завтра до 90 дней) и время по желанию;
  // без «Своего» поля даты не видно
  const due = page.locator('[data-due]');
  await expect(due.locator('[data-due-panel]')).toBeHidden();
  await due.getByRole('button', { name: 'Неделя' }).click();
  await expect(due.getByRole('button', { name: 'Неделя' })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('input[name="publishBy"]')).toHaveValue(dateIn(7).ru);
  await due.getByRole('button', { name: 'Свой' }).click();
  await expect(due.locator('[data-due-panel]')).toBeVisible();
  await expect(due.getByRole('button', { name: 'Свой' })).toHaveAttribute('aria-pressed', 'true');
  await expect(due.getByRole('button', { name: dateIn(0).label, exact: true })).toBeDisabled(); // сегодня — нельзя
  if (dateIn(10).month !== dateIn(7).month) await due.getByRole('button', { name: 'Следующий месяц' }).click();
  await due.getByRole('button', { name: dateIn(10).label, exact: true }).click();
  await expect(due.getByRole('button', { name: dateIn(10).label, exact: true })).toHaveAttribute('aria-pressed', 'true');
  await page.getByLabel('Время').fill('1830');
  await expect(page.getByLabel('Время')).toHaveValue('18:30');
  await expect(due.locator('[data-due-sum]')).toContainText('18:30 по Москве');
  await expect(page.locator('input[name="publishBy"]')).toHaveValue(dateIn(10).ru);
  await expect(page.locator('[data-err="publishBy"]')).toBeHidden();
  await expect(left).toContainText('Осталось: описание');

  // блоки: пункт — сразу поле (текст без Enter тоже сохраняется), Enter — следующий, Alt+↑ — выше
  const blocks = page.locator('[data-blocks] .bk');
  await expect.poll(() => vals(blocks.locator('.bk-title'))).toEqual(['Описание', 'Что показать']);
  await blocks.nth(0).locator('.bi-in').first().fill('Снимите завтрак при утреннем свете');
  await expect(left).toBeHidden();
  await blocks.nth(1).locator('.bi-in').first().fill('Интерьер');
  await page.keyboard.press('Enter');
  await page.keyboard.type('Сырники');
  await expect.poll(() => vals(blocks.nth(1).locator('.bi-in'))).toEqual(['Интерьер', 'Сырники']);
  await page.keyboard.press('Alt+ArrowUp');
  await expect.poll(() => vals(blocks.nth(1).locator('.bi-in'))).toEqual(['Сырники', 'Интерьер']);
  await expect(blocks.nth(1).locator('.bi-in').first()).toBeFocused();
  // Enter в конце — новый пустой, Backspace в пустом — удалить
  await blocks.nth(1).locator('.bi-in').nth(1).press('End');
  await page.keyboard.press('Enter');
  await expect(blocks.nth(1).locator('.bi')).toHaveCount(3);
  await page.keyboard.press('Backspace');
  await expect(blocks.nth(1).locator('.bi')).toHaveCount(2);
  // шаблон одним нажатием; вставка нескольких строк — несколько пунктов (маркеры списка убираются)
  const tpl = page.locator('.bk-tpl');
  await expect(tpl.getByRole('button')).toHaveText(['Требования', 'Формат', 'Сроки', 'Свой блок']);
  await tpl.getByRole('button', { name: 'Требования' }).click();
  await expect(blocks).toHaveCount(3);
  await expect(blocks.nth(2).locator('.bi-in').first()).toBeFocused();
  await paste(page, '[data-blocks] .bk:nth-child(3) .bi-in', 'Без мата\n- Вертикальное видео\n2) Отметить нас');
  await expect.poll(() => vals(blocks.nth(2).locator('.bi-in'))).toEqual(['Без мата', 'Вертикальное видео', 'Отметить нас']);
  await expect(tpl.getByRole('button', { name: 'Требования' })).toHaveCount(0);
  // свой блок: заголовок в строке, Enter — к пунктам; Alt+↑ в заголовке — блок выше
  await tpl.getByRole('button', { name: 'Свой блок' }).click();
  await expect(blocks.nth(3).locator('.bk-title')).toBeFocused();
  await page.keyboard.type('Хештеги');
  await page.keyboard.press('Enter');
  await page.keyboard.type('#зерно');
  await blocks.nth(3).locator('.bk-title').focus();
  await page.keyboard.press('Alt+ArrowUp');
  await expect.poll(() => vals(blocks.locator('.bk-title'))).toEqual(['Описание', 'Что показать', 'Хештеги', 'Требования']);
  // удалить блок — «Вернуть»
  await blocks.nth(3).getByRole('button', { name: 'Удалить блок' }).click();
  await expect(blocks).toHaveCount(3);
  await expect(page.locator('[data-toast]')).toContainText('Блок удалён');
  await page.locator('[data-toast-act]').click();
  await expect(blocks).toHaveCount(4);
  await blocks.nth(3).getByRole('button', { name: 'Удалить блок' }).click();
  await expect(blocks).toHaveCount(3);

  // обложка — с кадрированием 16:9; два фото «что показать»
  await page.locator('[data-cover] input[type=file]').setInputFiles(fixture('photo.jpg'));
  await page.getByRole('dialog', { name: 'Кадрирование' }).getByRole('button', { name: 'Готово' }).click();
  await expect(page.locator('[data-cover] .up-t[data-id]')).toHaveCount(1, { timeout: 15_000 });
  await page.locator('[data-photos] input[type=file]').setInputFiles([fixture('photo.jpg'), fixture('portrait.jpg')]);
  await expect(page.locator('[data-photos] .up-t[data-id]')).toHaveCount(2, { timeout: 20_000 });
  await saved(page);
  await expect.poll(async () => (await (await page.request.get(`/api/me/tasks/${id}`)).json()).photos.length, { timeout: 10_000 }).toBe(2);

  const draft = await (await page.request.get(`/api/me/tasks/${id}`)).json();
  expect(draft.status).toBe('draft');
  expect(draft.formats).toEqual(['reels', 'shorts', 'Прямой эфир']);
  expect(draft.categories).toEqual(slugs);
  expect(draft.access).toBe('approval');
  expect(draft.rewardKopecks).toBe(300000);
  expect(draft.publishBy).toBe(dateIn(10).iso);
  expect(draft.publishTime).toBe('18:30');
  expect(draft.deadlineAt).toBeTruthy();
  expect(draft.minLikes).toBeUndefined();
  expect(draft.blocks.map((b: { title: string }) => b.title)).toEqual(['Описание', 'Что показать', 'Хештеги']);
  expect(draft.blocks[0].items).toEqual(['Снимите завтрак при утреннем свете']);
  expect(draft.blocks[1].items).toEqual(['Сырники', 'Интерьер']);
  expect(draft.blocks[2].items).toEqual(['#зерно']);
  expect(draft.photos).toHaveLength(2);
  // фото черновика — только владельцу
  const guest = await browser.newContext();
  expect((await guest.request.get(new URL(draft.cover.src, page.url()).toString())).status()).toBe(404);
  await guest.close();

  // предпросмотр — как увидят авторы
  await page.getByRole('tab', { name: 'Как увидят' }).click();
  const pv = page.locator('[data-preview]');
  await expect(pv.locator('.it-t')).toHaveText('Сторис о новом осеннем меню');
  await expect(pv).toContainText(/3\s000\s₽/);
  await expect(pv.locator('.body-b h3')).toHaveText(['Описание', 'Что показать', 'Хештеги']);
  await page.getByRole('tab', { name: 'Правка' }).click();

  // опубликовать — проверка автоматическая: сразу «Опубликовано», в журнале — «одобрено автоматически»
  await submit.click();
  await page.waitForURL(`**/app/tasks/${id}?sent=1`);
  await expect(page.locator('[data-status-label]')).toHaveText('Опубликовано');
  await expect(page.locator('.banner.is-ok')).toContainText('Опубликовано');
  expect((await (await page.request.get(`/api/me/tasks/${id}`)).json()).status).toBe('published');
  await page.goto('/app/tasks');
  await expect(page.locator('.it', { hasText: 'Сторис о новом осеннем меню' }).locator('.st')).toHaveText('Опубликовано');

  // другой пользователь (автор) видит задание в ленте карточкой; отбор по городу
  const other = await otherUser(browser, info, 'task-viewer', 'creator');
  await other.goto(`/app?city=${encodeURIComponent(city)}`);
  await expect(other.getByRole('heading', { level: 1, name: 'Задания' })).toBeVisible();
  const tile = other.locator(`[data-tile="${id}"]`);
  await expect(tile.locator('.tile-t')).toHaveText('Сторис о новом осеннем меню');
  await expect(tile.locator('.tile-m')).toContainText(city);
  await expect(tile.locator('.tile-m')).toContainText(/до \d+ [а-я]+, 18:30/);
  await expect(tile.locator('.tile-ph img')).toBeVisible();
  await expectTileSummary(other, tile, /3\s000\s₽/, info);
  await other.goto('/app?city=Нетакогогорода');
  await expect(other.locator(`[data-tile="${id}"]`)).toHaveCount(0);
  // отбор по нескольким сферам — подходит любая из выбранных
  await other.goto(`/app?city=${encodeURIComponent(city)}&category=${slugs[1]}&category=nosuch-cat`);
  await expect(other.locator(`[data-tile="${id}"]`)).toHaveCount(1);
  await expect(other.locator('.pk-btn').first()).toHaveAttribute('data-on', '1');
  // нажатие — страница задания целиком
  await other.goto(`/app?city=${encodeURIComponent(city)}`);
  await other.locator(`[data-tile="${id}"] a`).click();
  await other.waitForURL(`**/app/task/${id}`);
  await expect(other.getByRole('heading', { level: 1, name: 'Сторис о новом осеннем меню' })).toBeVisible();
  await expect(other.locator('.terms')).toContainText(/3\s000\s₽/);
  await expect(other.locator('.terms')).toContainText(/до \d+ [а-я]+, 18:30/);
  await expect(other.locator('.terms')).not.toContainText('лайк');
  await expect(other.locator('.body-b h3')).toHaveText(['Описание', 'Что показать', 'Хештеги']);
  await expect(other.locator('.terms')).toContainText('Reels, Shorts, Прямой эфир');
  // фото — поверх страницы: счётчик, стрелки и клавиши, миниатюры; Esc — закрыть, фокус — обратно; без новых вкладок
  const photo2 = other.locator('.body-ph [data-lb]').first();
  await photo2.click();
  const lb = other.getByRole('dialog', { name: 'Просмотр фото' });
  await expect(lb).toBeVisible();
  await expect(lb.locator('.lb-count')).toHaveText('2 / 3'); // обложка — первая
  await expect(lb.locator('.lb-th')).toHaveCount(3);
  await expect(lb.locator('.lb-th').nth(1)).toHaveAttribute('aria-current', 'true');
  await other.keyboard.press('ArrowRight');
  await expect(lb.locator('.lb-count')).toHaveText('3 / 3');
  await expect(lb.getByRole('button', { name: 'Следующее фото' })).toBeDisabled();
  await lb.getByRole('button', { name: 'Предыдущее фото' }).click();
  await lb.getByRole('button', { name: 'Фото 1 из 3' }).click();
  await expect(lb.locator('.lb-count')).toHaveText('1 / 3');
  await expect(lb.locator('.lb-img')).toHaveAttribute('src', /\/media\/.+\/800\.webp/);
  await other.keyboard.press('Escape');
  await expect(lb).toBeHidden();
  await expect(photo2).toBeFocused();
  expect(other.context().pages()).toHaveLength(1);
  // щелчок по фону — закрыть
  await other.locator('.page-cover-b').click();
  await expect(lb.locator('.lb-count')).toHaveText('1 / 3');
  await lb.locator('.lb-stage').click({ position: { x: 4, y: 4 } });
  await expect(lb).toBeHidden();
  await other.getByRole('button', { name: 'Откликнуться' }).click();
  await expect(other.locator('[data-toast]')).toContainText('Отклики появятся');

  // снять и изменить — пропадает из ленты, снова правится
  await page.goto(`/app/tasks/${id}`);
  await page.getByRole('button', { name: 'Снять с публикации' }).click();
  await page.getByRole('dialog', { name: 'Снять с публикации?' }).getByRole('button', { name: 'Снять' }).click();
  await expect(page.getByLabel('Название задания')).toHaveValue('Сторис о новом осеннем меню');
  expect((await other.request.get(`/api/tasks/${id}`)).status()).toBe(404);
  await other.context().close();
  expect(errors).toEqual([]);
});

test('черновик: сохраняется сам, ✕ закрывает, открывается из «Мои», удаляется', async ({ page }, info) => {
  const errors = jsErrors(page);
  await login(page, uniq(info.project.name, 'draft'));
  await page.goto('/app/tasks/new');
  await ready(page);
  await page.getByLabel('Название задания').fill('Черновик про кофе');
  await page.getByLabel('Просмотры от').fill('300');
  await page.locator('[data-blocks] .bk').first().locator('.bi-in').first().fill('Пока только идея');
  await taskSaved(page);
  // ✕ — сохраняет, если не успели, и закрывает
  await page.getByRole('link', { name: 'Закрыть' }).click();
  await page.waitForURL('**/app/tasks');
  await page.getByRole('button', { name: /Черновик/ }).click();
  const row = page.locator('.it', { hasText: 'Черновик про кофе' });
  await expect(row.locator('.st')).toHaveText('Черновик');
  await row.getByRole('link', { name: 'Изменить' }).click();
  await ready(page);
  await expect(page.getByLabel('Название задания')).toHaveValue('Черновик про кофе');
  await expect(page.getByLabel('Просмотры от')).toHaveValue('300');
  await expect(page.locator('[data-blocks] .bi-in').first()).toHaveValue('Пока только идея');
  // «Опубликовать» — ошибки у всех незаполненных, курсор — в первом
  await page.getByRole('button', { name: 'Опубликовать' }).click();
  await expect(page.locator('[data-err="categories"]')).toHaveText('Выберите сферу');
  await expect(page.locator('[data-err="reward"]')).toHaveText('Оплата — от 100 ₽');
  await expect(page.locator('[data-err="title"]')).toBeHidden();
  await expect(page.locator('[data-left]')).toContainText('и ещё');
  expect((await (await page.request.get('/api/me/tasks')).json()).tasks[0].status).toBe('draft');
  // удалить из «Мои»
  await page.goto('/app/tasks');
  await page.locator('.it', { hasText: 'Черновик про кофе' }).getByRole('button', { name: 'Удалить' }).click();
  await page.getByRole('dialog', { name: 'Удалить задание?' }).getByRole('button', { name: 'Удалить' }).click();
  await expect(page.locator('.it', { hasText: 'Черновик про кофе' })).toHaveCount(0);
  expect((await (await page.request.get('/api/me/tasks')).json()).tasks).toHaveLength(0);
  expect(errors).toEqual([]);
});

// Баг 09: анкета автора не отправлялась — текст «О себе» без Enter не считался, «О себе» было обязательным,
// кнопка была недоступна без объяснения. Задача 12: подписчики — только из подтверждённой соцсети (Telegram — ботом).
test('анкета автора: из профиля, текст без Enter, без блоков → сразу опубликована, видна бизнесу', async ({ page, browser, request, context }, info) => {
  test.slow();
  test.skip(!(await botOn(request)), 'подтверждение Telegram — нужен стек make e2e-up');
  await context.route('https://t.me/**', (r) => r.fulfill({ body: 'telegram' }));
  const errors = jsErrors(page);
  const city = `Самара ${tag()}`;
  await login(page, uniq(info.project.name, 'card'));
  const h = await csrf(page);
  expect((await page.request.patch('/api/me/mode', { headers: h, data: { mode: 'creator' } })).status()).toBe(200);
  // как у заказчика: профиль автора из первого входа — темы и каналы (ссылка и @ник)
  expect((await page.request.put('/api/me/profile/creator', { headers: h, data: { name: 'Алина', city, spheres: ['food', 'beauty'], channels: ['https://t.me/alina', '@alina_ig'] } })).status()).toBe(200);
  await page.goto('/app/card');
  await ready(page);
  await expect(page.locator('.rail:visible, .bn:visible')).toHaveCount(1);
  await expect(page.getByLabel('Имя')).toHaveValue('Алина');
  await expect(page.locator('[data-cats] [aria-pressed="true"]')).toHaveCount(2);
  // соцсеть не подтверждена — «Осталось: соцсети», «Опубликовать» ведёт к кнопке «Подключить»
  await expect(page.locator('[data-left]')).toContainText('соцсети');
  const submit = page.getByRole('button', { name: 'Опубликовать' });
  await submit.click();
  await expect(page.locator('[data-err="platforms"]')).toHaveText('Подтвердите хотя бы одну соцсеть');
  await expect(page.locator('[data-platform="telegram"] [data-act]')).toBeFocused();
  await connectTelegram(page, request, tgUser(), { id: chatId(), type: 'channel', title: 'Алина ест', username: 'alina_eats', members: 12_000 });
  await expect(page.locator('[data-err="platforms"]')).toBeHidden();
  await expect(page.locator('[data-left]')).toBeHidden();
  // своя тема (подсказки похожих)
  await page.getByRole('button', { name: 'Своя' }).click();
  const cb = page.getByRole('combobox', { name: 'Своя категория' });
  await cb.fill('Настольные игры');
  await page.getByRole('option', { name: 'Добавить «Настольные игры»' }).click();
  await expect(page.locator('[data-cats] .chip.is-own[aria-pressed="true"]')).toContainText('Настольные игры');
  // «О себе» — текст без Enter
  await page.locator('[data-blocks] .bk').first().locator('.bi-in').first().fill('Снимаю еду и городские места');
  // фото
  await page.locator('[data-cover] input[type=file]').setInputFiles(fixture('portrait.jpg'));
  await page.getByRole('dialog', { name: 'Кадрирование' }).getByRole('button', { name: 'Готово' }).click();
  await page.locator('[data-photos] input[type=file]').setInputFiles([fixture('photo.jpg'), fixture('portrait.jpg')]);
  await expect(page.locator('[data-cover] .up-t[data-id], [data-photos] .up-t[data-id]')).toHaveCount(3, { timeout: 20_000 });
  await saved(page);
  await expect.poll(async () => (await (await page.request.get('/api/me/card')).json()).card?.works.length, { timeout: 10_000 }).toBe(2);
  const card = (await (await page.request.get('/api/me/card')).json()).card;
  expect(card.platforms).toMatchObject([{ kind: 'telegram', type: 'channel', url: 'https://t.me/alina_eats', followers: 12000, verified: true }]);
  expect(card.blocks[0].items).toEqual(['Снимаю еду и городские места']);
  expect(card.topics).toHaveLength(3);

  await submit.click();
  await page.waitForURL('**/app/card?sent=1');
  await expect(page.locator('[data-status-label]')).toHaveText('Опубликовано');
  expect((await (await page.request.get('/api/me/card')).json()).card.status).toBe('published');

  // бизнес видит анкету в ленте; наведение — главное; нажатие — анкета целиком
  const biz = await otherUser(browser, info, 'card-viewer', 'business');
  await biz.goto(`/app?city=${encodeURIComponent(city)}`);
  await expect(biz.getByRole('heading', { level: 1, name: 'Авторы' })).toBeVisible();
  const tile = biz.locator(`[data-tile="${card.id}"]`);
  await expect(tile.locator('.tile-t')).toHaveText('Алина');
  await expectTileSummary(biz, tile, /12\s000/, info);
  await tile.locator('a').click();
  await biz.waitForURL(`**/app/creator/${card.id}`);
  await expect(biz.getByRole('heading', { level: 1, name: 'Алина' })).toBeVisible();
  await expect(biz.locator('.pv-tags')).toContainText('Настольные игры');
  await expect(biz.locator('.rows')).toContainText('Подтверждено');
  await expect(biz.locator('.rows .row-v')).toHaveText(/12\s000/);
  // портфолио — просмотр поверх страницы, листание
  await biz.locator('.body-ph [data-lb]').nth(1).click();
  const lb = biz.getByRole('dialog', { name: 'Просмотр фото' });
  await expect(lb.locator('.lb-count')).toHaveText('2 / 2');
  await biz.keyboard.press('ArrowLeft');
  await expect(lb.locator('.lb-count')).toHaveText('1 / 2');
  await lb.getByRole('button', { name: 'Закрыть' }).click();
  await expect(lb).toBeHidden();
  await biz.getByRole('button', { name: 'Предложить задание' }).click();
  await expect(biz.locator('[data-toast]')).toContainText('Предложения появятся');
  // фото опубликованной анкеты — всем
  expect((await biz.request.get(card.works[0].src)).status()).toBe(200);
  await biz.context().close();

  // снять и изменить
  await page.getByRole('button', { name: 'Снять с публикации' }).click();
  await page.getByRole('dialog', { name: 'Снять с публикации?' }).getByRole('button', { name: 'Снять' }).click();
  await expect(page.getByLabel('Имя')).toHaveValue('Алина');
  expect(errors).toEqual([]);
});

// Заполненное не теряется при обновлении: черновик формы — на сервере (Redis / PostgreSQL) и копия на устройстве.
test('черновик формы: заполнить → обновить → всё на месте; без связи — копия на устройстве', async ({ page }, info) => {
  const errors = jsErrors(page);
  await login(page, uniq(info.project.name, 'restore'));
  await page.goto('/app/tasks/new');
  await ready(page);
  // ещё не проходит проверку сервера (название короткое) — но черновик формы сохраняется
  await page.getByLabel('Название задания').fill('Кофе');
  await page.locator('[data-cats] [data-slug]').nth(1).click();
  await page.getByRole('button', { name: 'TikTok' }).click();
  await page.locator('[data-formats]').getByRole('button', { name: 'Свой' }).click();
  await page.getByRole('combobox', { name: 'Свой формат' }).fill('Распаковка кофе');
  await page.getByRole('option', { name: 'Добавить «Распаковка кофе»' }).click();
  await page.locator('[data-due]').getByRole('button', { name: '2 недели' }).click();
  await page.getByLabel('Просмотры от').fill('7000');
  const blocks = page.locator('[data-blocks] .bk');
  await blocks.nth(1).locator('.bi-in').first().fill('Бариста');
  await page.keyboard.press('Enter');
  await page.keyboard.type('Латте-арт');
  await page.keyboard.press('Alt+ArrowUp');
  await page.locator('.bk-tpl').getByRole('button', { name: 'Сроки' }).click();
  await page.keyboard.type('До выходных');
  await saved(page);
  await expect(page.locator('[data-state]')).toHaveText('Сохранено · только что');
  const slug = await page.locator('[data-cats] [data-slug]').nth(1).getAttribute('data-slug');

  await page.reload();

  await ready(page);
  await expect(page.getByLabel('Название задания')).toHaveValue('Кофе');
  await expect(page.getByLabel('Просмотры от')).toHaveValue('7 000');
  await expect(page.getByRole('button', { name: 'TikTok' })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('[data-formats] .chip.is-own')).toHaveText('Распаковка кофе');
  await expect(page.locator('[data-formats] .chip.is-own')).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('[data-due]').getByRole('button', { name: '2 недели' })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('[data-due-panel]')).toBeHidden();
  await expect(page.locator(`[data-cats] [data-slug="${slug}"]`)).toHaveAttribute('aria-pressed', 'true');
  await expect.poll(() => vals(page.locator('[data-blocks] .bk-title'))).toEqual(['Описание', 'Что показать', 'Сроки']);
  await expect.poll(() => vals(page.locator('[data-blocks] .bk').nth(1).locator('.bi-in'))).toEqual(['Латте-арт', 'Бариста']);
  await expect(page.locator('[data-state]')).toContainText('Сохранено');

  // нет связи с сервером черновиков — копия на устройстве; связь вернулась — уходит на сервер
  await page.route('**/api/me/drafts/**', (r) => (r.request().method() === 'GET' ? r.continue() : r.abort()));
  await page.route('**/api/me/tasks**', (r) => r.abort());
  await page.getByLabel('Название задания').fill('Кофе с собой');
  await expect(page.locator('[data-state]')).toHaveText('Нет связи — сохранено на устройстве', { timeout: 10_000 });
  await page.unrouteAll({ behavior: 'ignoreErrors' });
  await page.reload();
  await ready(page);
  await expect(page.getByLabel('Название задания')).toHaveValue('Кофе с собой');
  const form = page.url().match(/\/app\/tasks\/(\d+)/) ? `task-${page.url().split('/').pop()}` : 'task-new';
  await expect.poll(async () => (await (await page.request.get(`/api/me/drafts/${form}`)).json()).draft?.data.fields.title, { timeout: 10_000 }).toBe('Кофе с собой');

  // анкета — так же
  await page.request.patch('/api/me/mode', { headers: await csrf(page), data: { mode: 'creator' } });
  await page.goto('/app/card');
  await ready(page);
  await page.getByLabel('Город').fill('Тверь');
  await page.locator('[data-blocks] .bk').first().locator('.bi-in').first().fill('Снимаю кофейни');
  await saved(page);
  await page.reload();
  await ready(page);
  await expect(page.getByLabel('Город')).toHaveValue('Тверь');
  await expect(page.locator('[data-blocks] .bk').first().locator('.bi-in').first()).toHaveValue('Снимаю кофейни');
  expect(errors).toEqual([]);
});

test('фото: понятные ошибки типа и размера, отмена загрузки', async ({ page }, info) => {
  await login(page, uniq(info.project.name, 'up'));
  await page.goto('/app/tasks/new');
  await ready(page);
  const input = page.locator('[data-photos] input[type=file]');
  await input.setInputFiles(fixture('anim.gif'));
  await expect(page.locator('[data-toast]')).toContainText('JPEG, PNG, WebP или HEIC');
  await input.setInputFiles({ name: 'big.jpg', mimeType: 'image/jpeg', buffer: Buffer.alloc(11 << 20, 1) });
  await expect(page.locator('[data-toast]')).toContainText('больше 10 МБ');
  await expect(page.locator('[data-photos] .up-t')).toHaveCount(0);
  // не фото под видом JPEG — отказ сервера у плитки
  await input.setInputFiles({ name: 'fake.jpg', mimeType: 'image/jpeg', buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"/>') });
  await expect(page.locator('[data-photos] .up-t.is-error .up-err')).toContainText('JPEG, PNG, WebP или HEIC');
  // отмена: медленная сеть — «Отменить загрузку»
  await page.route('**/api/me/media', async (r) => {
    await new Promise((res) => setTimeout(res, 3000));
    await r.continue().catch(() => {});
  });
  await input.setInputFiles(fixture('photo.jpg'));
  await page.locator('[data-photos] .up-t.is-loading').getByRole('button', { name: 'Отменить загрузку' }).click();
  await expect(page.locator('[data-photos] .up-t.is-loading')).toHaveCount(0);
});

test('сервер проверяет сам: неполное — 400 по полям, чужое — 404, лишнее поле — 400', async ({ page }, info) => {
  await login(page, uniq(info.project.name, 'api'));
  const csrf = (await (await page.request.get('/api/auth/config')).json()).csrfToken;
  const h = { 'X-CSRF-Token': csrf };
  const base = { title: '', city: '', nationwide: false, categories: [] as string[], formats: [] as string[], access: 'open', minViews: 0, rewardKopecks: 0, publishBy: '', blocks: [], cover: '', photos: [] };
  const created = await page.request.post('/api/me/tasks', { headers: h, data: base });
  expect(created.status()).toBe(201);
  const { id } = await created.json();
  const sub = await page.request.post(`/api/me/tasks/${id}/submit`, { headers: h });
  expect(sub.status()).toBe(400);
  const f = (await sub.json()).error.fields;
  for (const k of ['title', 'city', 'categories', 'formats', 'minViews', 'reward', 'publishBy', 'blocks']) expect(f[k], k).toBeTruthy();
  // свои форматы: нормализуются, до 3; сфер — до 5
  const norm = await page.request.put(`/api/me/tasks/${id}`, { headers: h, data: { ...base, formats: [' Reels', '  прямой   эфир ', 'обзор кофе', 'reels'] } });
  expect((await norm.json()).formats).toEqual(['reels', 'Прямой эфир', 'Обзор кофе']);
  const many = await page.request.put(`/api/me/tasks/${id}`, { headers: h, data: { ...base, formats: ['reels', 'shorts', 'tiktok', 'Обзор'] } });
  expect((await many.json()).error.fields.formats).toBe('Не больше 3 форматов');
  const link = await page.request.put(`/api/me/tasks/${id}`, { headers: h, data: { ...base, formats: ['vk.com/x'] } });
  expect((await link.json()).error.fields.formats).toBeTruthy();
  const cats = await page.request.put(`/api/me/tasks/${id}`, { headers: h, data: { ...base, categories: ['a', 'b', 'c', 'd', 'e', 'f'] } });
  expect((await cats.json()).error.fields.categories).toBe('Не больше 5 сфер');
  const hints = await (await page.request.get('/api/me/formats/similar?q=' + encodeURIComponent('обзор'))).json();
  expect(hints.items.map((x: { title: string }) => x.title)).toEqual(expect.arrayContaining(['Обзор', 'Обзор кофе']));
  const bad = await page.request.put(`/api/me/tasks/${id}`, { headers: h, data: { ...base, blocks: [{ kind: 'links', title: 'x', items: ['javascript:alert(1)'] }] } });
  expect((await bad.json()).error.fields['blocks.0']).toBeTruthy();
  expect((await page.request.put(`/api/me/tasks/${id}`, { headers: h, data: { ...base, status: 'published' } })).status()).toBe(400);
  expect((await page.request.get('/api/me/tasks/999999999')).status()).toBe(404);
});
