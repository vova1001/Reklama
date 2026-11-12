// Для каждой страницы: если есть cookie сессии — узнаём, кто вошёл (шапка, приложение).
// /admin — только сотрудникам (остальным — 404, без подсказки, что раздел есть); без подтверждённой 2FA —
// только страница 2FA. Сервер проверяет роль и 2FA и на каждом запросе к API — здесь только маршрут страницы.
import { defineMiddleware } from 'astro:middleware';
import { fetchMe, SESSION_COOKIE } from './lib/auth';
import { adminGet, type AdminMe } from './lib/admin';

export const onRequest = defineMiddleware(async (ctx, next) => {
  ctx.locals.me = null;
  ctx.locals.admin = null;
  if (ctx.cookies.has(SESSION_COOKIE)) {
    ctx.locals.me = await fetchMe(ctx.request);
  }
  const path = ctx.url.pathname;
  if (path === '/admin' || path.startsWith('/admin/')) {
    if (!ctx.locals.me) return ctx.redirect('/login', 303);
    const r = ctx.locals.me.user.staffRole ? await adminGet<AdminMe>(ctx.request, '/api/admin/me') : null;
    if (!r || !r.ok) {
      if (r && r.status === 503) return new Response('API недоступно', { status: 503, headers: { 'Cache-Control': 'no-store' } });
      return ctx.rewrite('/404');
    }
    ctx.locals.admin = r.data;
    const tf = r.data.twoFactor;
    if ((!tf.enabled || !tf.verified) && path !== '/admin/2fa') return ctx.redirect('/admin/2fa', 303);
    const res = await next();
    res.headers.set('Cache-Control', 'private, no-store');
    res.headers.set('X-Frame-Options', 'DENY');
    res.headers.set('X-Robots-Tag', 'noindex, nofollow');
    res.headers.set('Referrer-Policy', 'same-origin');
    return res;
  }
  return next();
});
