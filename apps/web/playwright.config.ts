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
 *
 * That drives the dev server. To drive the console the stack itself serves,
 * which is the production build a self-hoster runs (ENT-241):
 *
 *     KINDLAST_WEB_URL=http://localhost:8000 bun run --cwd apps/web test:e2e
 *
 * In a git worktree running its own stack (ENT-250) the edge is not on 8000,
 * and `scripts/stack-env.sh` puts its address in KINDLAST_EDGE_URL:
 *
 *     eval "$(./scripts/stack-env.sh)"
 *     KINDLAST_WEB_URL="$KINDLAST_EDGE_URL" bun run --cwd apps/web test:e2e
 *
 * KINDLAST_WEB_URL is deliberately not derived for you. It carries a second
 * meaning below, "a console is already running, do not start one", so a script
 * that set it silently would stop the dev-server path from ever running.
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

  // A dev server, but only when nobody named a console to drive.
  //
  // KINDLAST_WEB_URL means "this one is already running", and it is how the
  // suite is pointed at the container the compose stack serves. Starting
  // `bun run dev` in that case would compile the app a second time, bind port
  // 3000 nobody asked for, and leave the run testing whichever of the two
  // answered first.
  webServer: process.env.KINDLAST_WEB_URL
    ? undefined
    : {
        command: 'bun run dev',
        url: 'http://localhost:3000',
        reuseExistingServer: !process.env.CI,
        timeout: 120_000,
      },
})
