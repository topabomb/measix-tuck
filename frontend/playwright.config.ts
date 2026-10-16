import { defineConfig, devices } from '@playwright/test'

const remoteBaseURL = process.env.PLAYWRIGHT_BASE_URL?.replace(/\/?$/, '/')

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  expect: { timeout: 12_000 },
  retries: 0,
  // Native Agent processes and million-row frame tests share the same host.
  // Bound concurrent browsers so the performance contract measures the UI,
  // not contention from eight independent stress workloads.
  workers: 2,
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: remoteBaseURL ?? 'http://127.0.0.1:4173/',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{
    name: remoteBaseURL ? 'pages-chromium' : 'chromium',
    use: {
      ...devices['Desktop Chrome'],
      ...(process.env.CI ? { channel: 'chrome' as const } : {}),
    },
  }],
  webServer: remoteBaseURL ? undefined : {
    command: 'pnpm exec vite preview --host 127.0.0.1 --port 4173',
    url: 'http://127.0.0.1:4173',
    timeout: 15_000,
    reuseExistingServer: !process.env.CI,
  },
})
