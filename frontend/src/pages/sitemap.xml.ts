import type { APIRoute } from 'astro';

// Пока на сайте одна настоящая страница — главная.
export const GET: APIRoute = () => {
  const site = process.env.PUBLIC_URL ?? 'http://localhost:8000';
  const xml = `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>${site}/</loc></url></urlset>\n`;
  return new Response(xml, { headers: { 'Content-Type': 'application/xml; charset=utf-8' } });
};
