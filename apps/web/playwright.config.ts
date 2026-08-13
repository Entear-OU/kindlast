import { defineConfig, devices } from '@playwright/test'

/**
 * End-to-end tests (ENT-197).
 *
 * These exist because the authorization code flow is precisely what unit tests
 * cannot cover. It is a redirect to a login form this application does not
 * own, a submission, a redirect back carrying a code, a cookie being set, and
 * a session surviving the next navigation. Every step is real or the test
 * proves nothing, which is the same reasoning §13.2 applies to token
 * verification on the Go side.
 *
 * They need the compose stack for Zitadel and Redis:
 *
 *     docker compose -f deploy/compose.yaml up -d
 *     bun run --cwd apps/web test:e2e
 */
export default defineConfig({
  testDir: './e2e',

  // A failing auth test is usually a redirect landing somewhere unexpected,
  // and the trace is the only artefact that shows where.
  use: {
    baseURL: process.env.KINDLAST_WEB_URL ?? 'http://localhost:3000',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },

  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],

  // Serial in CI. These tests share one Zitadel and one Redis, and a sign-out
  // in one worker would end the session another is using.
  workers: process.env.CI ? 1 : undefined,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'list',

  webServer: {
    command: 'bun run dev',
    url: process.env.KINDLAST_WEB_URL ?? 'http://localhost:3000',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
})
