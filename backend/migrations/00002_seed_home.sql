-- +goose Up
-- Начальные данные главной (макет «Охват», круг 4; цены — от заказчика 2026-11-07).
INSERT INTO categories (slug, title, icon, position) VALUES
    ('food',      'Еда и кафе',       'coffee',         1),
    ('beauty',    'Красота и салоны', 'scissors',       2),
    ('sport',     'Спорт и фитнес',   'dumbbell',       3),
    ('flowers',   'Цветы и подарки',  'flower-2',       4),
    ('fashion',   'Мода',             'shirt',          5),
    ('kids',      'Дети',             'baby',           6),
    ('pets',      'Животные',         'paw-print',      7),
    ('tech',      'Техника',          'smartphone',     8),
    ('auto',      'Авто',             'car',            9),
    ('home',      'Дом и ремонт',     'house',         10),
    ('education', 'Образование',      'graduation-cap',11),
    ('travel',    'Путешествия',      'plane',         12);

INSERT INTO plans (audience, slug, name, subtitle, price_kopecks, recommended, position) VALUES
    ('business', 'free', 'Бесплатный', 'Попробовать сервис',          0,       false, 1),
    ('business', 'pro',  'Про',        'Для регулярной рекламы',      300000,  true,  2),
    ('business', 'max',  'Максимум',   'Охват и данные по максимуму', 1000000, false, 3),
    ('creator',  'free', 'Бесплатный', 'Чтобы начать',                0,       false, 1),
    ('creator',  'pro',  'Про',        'Для тех, кто снимает часто',  300000,  true,  2),
    ('creator',  'top',  'Топ',        'Больше всего заказов',        1000000, false, 3);

INSERT INTO plan_features (plan_id, position, text)
SELECT p.id, f.position, f.text
FROM (VALUES
    ('business', 'free', 1, 'Размещение 7 дней'),
    ('business', 'free', 2, 'Обычная карточка задания'),
    ('business', 'pro',  1, 'Размещение 30 дней'),
    ('business', 'pro',  2, 'Топ ленты 3 дня'),
    ('business', 'pro',  3, 'Своё оформление карточки'),
    ('business', 'max',  1, 'Размещение 60 дней'),
    ('business', 'max',  2, 'Топ ленты 7 дней'),
    ('business', 'max',  3, 'Статистика: просмотры по городам, цена за 1000 просмотров, сравнение авторов'),
    ('business', 'max',  4, 'Выгрузка отчётов по заданиям'),
    ('business', 'max',  5, 'Личный подбор авторов'),
    ('creator',  'free', 1, '10 откликов в месяц'),
    ('creator',  'pro',  1, '50 откликов в месяц'),
    ('creator',  'pro',  2, 'Своё оформление профиля'),
    ('creator',  'pro',  3, 'Значок «Про» в каталоге'),
    ('creator',  'top',  1, 'Отклики без лимита'),
    ('creator',  'top',  2, 'Место в топе каталога'),
    ('creator',  'top',  3, 'Ранний доступ к новым заданиям')
) AS f(audience, slug, position, text)
JOIN plans p ON p.audience = f.audience AND p.slug = f.slug;

INSERT INTO faq (question, answer, position) VALUES
    ('Как сервис проверяет выполнение?',
     'Автор загружает ссылку на ролик. Сервис сверяет его с ТЗ и условиями — просмотры, лайки — и сам переводит оплату.', 1),
    ('Когда автор получает деньги?',
     'Сразу после проверки: как только ролик набрал условия задания, платёжная система автоматически переводит оплату автору.', 2),
    ('Что если условия не выполнены?',
     'Оплата автору не уходит — деньги остаются в безопасной сделке на счёте бизнеса.', 3),
    ('Можно выбрать автора самому?',
     'Да. Создайте задание «с одобрением»: заявки авторов придут вам, и вы сами выберете, кто будет снимать.', 4);

-- +goose Down
DELETE FROM faq;
DELETE FROM plan_features;
DELETE FROM plans;
DELETE FROM categories;
