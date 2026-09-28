import { defineConfig, devices } from '@playwright/test';

// gummi web's end-to-end suite. Two kinds of test live here:
//
//   harness/  proves the harness itself (the scripted agent, the seeding
//             helpers, fake gh) with the real CLI and no browser. Runs once,
//             in the "harness" project.
//   specs/    drives the web UI in headless Chromium, once per viewport
//             project (desktop, laptop, phone).
//
// Every test gets its own temp workspace and, when it asks for one, its own
// `gummi web` server, so tests never share state; workers stay at 1 anyway
// because each server runs a whole board (agents, checks, git) and the
// container is small.
export default defineConfig({
  globalSetup: './fixtures/global-setup.ts',
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: !!process.env.CI,
  reporter: [['list'], ['html', { outputFolder: 'report', open: 'never' }]],
  outputDir: 'test-results',
  use: {
    headless: true,
    trace: 'off',
    screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'harness', testDir: './harness' },
    {
      name: 'desktop',
      testDir: './specs',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } },
    },
    {
      name: 'laptop',
      testDir: './specs',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1024, height: 768 } },
    },
    {
      name: 'phone',
      testDir: './specs',
      use: {
        ...devices['Desktop Chrome'],
        viewport: { width: 390, height: 844 },
        deviceScaleFactor: 3,
        isMobile: true,
        hasTouch: true,
      },
    },
  ],
});
