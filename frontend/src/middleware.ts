// Для каждой страницы: если есть cookie сессии — узнаём, кто вошёл (шапка, кабинет, анкета).
import { defineMiddleware } from 'astro:middleware';
import { fetchMe, SESSION_COOKIE } from './lib/auth';

export const onRequest = defineMiddleware(async (ctx, next) => {
  ctx.locals.me = null;
  if (ctx.cookies.has(SESSION_COOKIE)) {
    ctx.locals.me = await fetchMe(ctx.request.headers.get('cookie') ?? '');
  }
  return next();
});
