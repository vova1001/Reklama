import { defineConfig, devices } from '@playwright/test';

// e2e против поднятого сайта: по умолчанию docker compose (http://localhost:8000), иначе BASE_URL.
export default defineConfig({
  testDir: 'tests/e2e',
  timeout: 30_000,
  retries: 0,
  reporter: [['list']],
  use: { baseURL: process.env.BASE_URL ?? 'http://localhost:8000', trace: 'retain-on-failure' },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } },
    { name: 'tablet', use: { ...devices['Desktop Chrome'], viewport: { width: 768, height: 1024 } } },
    { name: 'mobile', use: { ...devices['Pixel 7'], viewport: { width: 375, height: 760 } } },
  ],
});
