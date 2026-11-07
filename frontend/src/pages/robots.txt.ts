import type { APIRoute } from 'astro';

export const GET: APIRoute = () => {
  const site = process.env.PUBLIC_URL ?? 'http://localhost:8000';
  return new Response(`User-agent: *\nAllow: /\n\nSitemap: ${site}/sitemap.xml\n`, {
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  });
};
