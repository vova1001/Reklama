import { defineConfig } from 'astro/config';
import node from '@astrojs/node';
import tailwindcss from '@tailwindcss/vite';

// Главная рендерится на сервере: данные (темы, тарифы, вопросы) — из API бэкенда при каждом запросе (с кэшем).
export default defineConfig({
  output: 'server',
  adapter: node({ mode: 'standalone' }),
  server: { host: true, port: 4321 },
  devToolbar: { enabled: false },
  compressHTML: true,
  build: { inlineStylesheets: 'always' },
  vite: { plugins: [tailwindcss()] },
});
