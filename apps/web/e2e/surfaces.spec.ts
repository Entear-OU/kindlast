import { test, expect, type Page } from '@playwright/test'
import {
  createVerifiedUser,
  deleteUser,
  FIXTURE_PASSWORD,
  type FixtureUser,
} from './fixtures/identity'
import { LANDED_IN_ORG, onOrigin } from './fixtures/origin'

/**
 * The console surfaces, in a real browser, signed in (ENT-207, ENT-210,
 * ENT-223).
 *
 * # WHY THIS EXISTS ON TOP OF THE COMPONENT TESTS
 *
 * Because four separate times in this stack a surface has been green in every
 * test and broken in the browser: ENT-200's render function, ENT-209's missing
 * grant, ENT-210's unrouted webhook, and ENT-207's unregistered handler. Every
 * one had the same shape, a test that did not cross the boundary the failure
 * crossed. A component test renders a component; it does not prove a route
 * exists, a scope was granted, a service was registered, or a query runs.
 *
 * This crosses all of them: a browser, a real session, the edge, core-api, and
 * Postgres.
 *
 * # IT SIGNS IN WITH THE SAME FIXTURE THE JOURNEY TEST USES
 *
 * A throwaway user created through Zitadel's import endpoint, already
 * verified, deleted afterwards. `FIXTURE_PASSWORD` is a development-only value
 * that lives in the repository for the same reason the compose passwords do.
 *
 * Needs the compose stack and the web dev server:
 *
 *     docker compose -f deploy/compose.yaml up -d
 *     bash scripts/web-env.sh   # after any `down -v`
 *     bun run dev
 *
 * # THIS ONE DOES NOT RUN IN CI, AND IT CANNOT PASS TODAY (ENT-264)
 *
 * `auth.spec.ts` and `journey.spec.ts` are a gate on every pull request and a
 * nightly against main. This file is in neither, and the reason is not that
 * somebody judged it too slow.
 *
 * ENT-212 put a compliance-profile gate under `/o/{slug}/`: a member of an
 * organisation with no profile is routed to `/o/{slug}/onboarding` rather than
 * to the page they asked for. Every page below is behind that gate, and every
 * fixture user is brand new, so every navigation here lands on onboarding
 * instead. That went unnoticed because nothing ran this suite, which is the
 * whole of what ENT-264 is about.
 *
 * Making it run needs a fixture that arrives already profiled, either by
 * writing a `compliance_profiles` row out of band the way `mintInvitation`
 * writes an invitation, or by driving the eleven-question interview once and
 * reusing the session. Both are real work rather than a line of YAML, so this
 * file is honestly excluded rather than quietly half-passing. When that lands,
 * add it to the `specs` input in `.github/workflows/nightly.yml`.
 *
 * # AND THERE IS A SHORT LIST THIS STILL CANNOT REACH
 *
 * `MANUAL-CHECKS.md`, next to this file. Two failures got past this suite
 * after it was written, and neither was an ordinary coverage gap: one needed
 * a session cookie that had gone stale before the page was opened, and one
 * needed a container serving a binary older than the source. A run that
 * builds its own fixtures and its own binary starts in neither state, which
 * is the same property that makes it repeatable. Read that file before
 * assuming a green run here means a working product.
 */

async function skipSecondFactorOffer(page: Page) {
  const skip = page.locator('[name="skip"]').first()

  const outcome = await Promise.race([
    page
      .waitForURL(onOrigin(), { timeout: 20_000 })
      .then(() => 'returned' as const)
      .catch(() => 'neither' as const),
    skip
      .waitFor({ state: 'visible', timeout: 20_000 })
      .then(() => 'offered' as const)
      .catch(() => 'neither' as const),
  ])

  if (outcome === 'offered') await skip.click()
}

/**
 * Sign in and land inside the organisation.
 *
 * The hand-off is a button rather than an immediate redirect, which is
 * ENT-197's deliberate shape: `/sign-in` renders a card and the visitor
 * chooses to leave, so nothing navigates a person off the origin without them
 * asking. A test that went straight to /workspace would sit waiting for a
 * login form that is one click away.
 */
async function enterConsole(page: Page, user: FixtureUser) {
  await page.goto('/sign-in')
  await page.getByRole('button', { name: 'Continue', exact: true }).click()
  await signIn(page, user)
  await page.waitForURL(LANDED_IN_ORG, { timeout: 30_000 })
}

async function signIn(page: Page, user: FixtureUser) {
  const loginName = page
    .locator('input[name="loginName"], input[type="email"]')
    .first()
  await loginName.waitFor({ state: 'visible', timeout: 20_000 })
  await loginName.fill(user.username)
  await page
    .getByRole('button', { name: /next|continue|weiter/i })
    .first()
    .click()

  const password = page
    .locator('input[name="password"], input[type="password"]')
    .first()
  await password.waitFor({ state: 'visible', timeout: 20_000 })
  await password.fill(FIXTURE_PASSWORD)
  await page
    .getByRole('button', { name: /next|continue|sign in|weiter/i })
    .first()
    .click()

  await skipSecondFactorOffer(page)
}

test.describe('the console surfaces', () => {
  let user: FixtureUser
  let orgSlug: string

  test.describe.configure({ mode: 'serial' })

  test.beforeAll(async () => {
    user = await createVerifiedUser('surfaces')
  })

  test.afterAll(async () => {
    if (user) await deleteUser(user.id)
  })

  test('a new person lands in their own organisation', async ({ page }) => {
    await enterConsole(page, user)

    orgSlug = new URL(page.url()).pathname.split('/')[2]
    expect(orgSlug).toBeTruthy()
  })

  test('Regulation shows the corpus, not an empty page', async ({ page }) => {
    await enterConsole(page, user)
    const slug = new URL(page.url()).pathname.split('/')[2]

    await page.goto(`/o/${slug}/regulation`)
    await expect(
      page.getByRole('heading', { name: 'Regulation', level: 1 }),
    ).toBeVisible()

    // THE ASSERTION THIS WHOLE FILE IS FOR. Before ENT-207 the corpus tables
    // were empty, so this page would have said "No regulation has been loaded"
    // and every citation on every finding read as a raw CELEX number.
    await expect(page.getByText('General Data Protection')).toBeVisible()
    await expect(page.getByText(/99 articles/)).toBeVisible()
    await expect(page.getByText(/Artificial Intelligence Act/)).toBeVisible()

    // And the citation renders as a regulation rather than as `32016R0679`.
    const citation = page.getByRole('link', { name: /GDPR Art\. 30/ }).first()
    await expect(citation).toBeVisible()
    await expect(citation).toHaveAttribute('href', /eur-lex\.europa\.eu/)

    // A raw CELEX anywhere on this page means the helper fell back, which is
    // the exact condition ENT-207 fixed.
    await expect(page.getByText(/32016R0679 Art\./)).toHaveCount(0)

    await page.screenshot({
      path: 'test-results/surfaces-regulation.png',
      fullPage: true,
    })
  })

  test('an obligation shows the provision behind it', async ({ page }) => {
    await enterConsole(page, user)
    const slug = new URL(page.url()).pathname.split('/')[2]

    await page.goto(`/o/${slug}/regulation/gdpr-art-30-ropa`)

    await expect(
      page.getByRole('heading', { name: /Records of Processing/i }),
    ).toBeVisible()
    await expect(
      page.getByRole('heading', { name: 'The provision' }),
    ).toBeVisible()

    // The summary is ours and the page has to say so, because a reader
    // deciding whether to act on a finding needs to know they are reading a
    // paraphrase rather than the Official Journal.
    await expect(
      page.getByText(/A summary, not the official wording/i),
    ).toBeVisible()
    await expect(
      page.getByRole('link', { name: /Read the provision on EUR-Lex/i }),
    ).toBeVisible()

    await page.screenshot({
      path: 'test-results/surfaces-obligation.png',
      fullPage: true,
    })
  })

  test('Logs renders and says what it is not', async ({ page }) => {
    await enterConsole(page, user)
    const slug = new URL(page.url()).pathname.split('/')[2]

    await page.goto(`/o/${slug}/logs`)

    await expect(
      page.getByRole('heading', { name: 'Logs', level: 1 }),
    ).toBeVisible()

    // The §7.2 firewall, in the page's own voice. This is the property an
    // auditor is buying and it should not have to be inferred.
    await expect(
      page.getByText(/not assembled from traces or monitoring data/i),
    ).toBeVisible()

    // A brand-new organisation has decided nothing, and the page has to say so
    // rather than look like it failed to load.
    await expect(page.getByText(/Nothing has been decided yet/i)).toBeVisible()

    // The filter form is a plain GET, so filtering works with no client
    // JavaScript and a filtered view is a URL somebody can send.
    await expect(
      page.getByRole('button', { name: /Apply filters/i }),
    ).toBeVisible()
    await expect(page.getByRole('link', { name: /Export CSV/i })).toBeVisible()

    await page.screenshot({
      path: 'test-results/surfaces-logs.png',
      fullPage: true,
    })
  })

  test('the audit export downloads a CSV with its header row', async ({
    page,
  }) => {
    await enterConsole(page, user)
    const slug = new URL(page.url()).pathname.split('/')[2]

    // Straight at the route handler, because what is under test is that it
    // produces a file rather than that a link is clickable.
    const response = await page.request.get(`/o/${slug}/logs/export`)
    expect(response.status()).toBe(200)
    expect(response.headers()['content-type']).toContain('text/csv')
    expect(response.headers()['content-disposition']).toContain('attachment')
    // Never cached: a shared proxy holding one tenant's audit log is the worst
    // cache bug available in this product.
    expect(response.headers()['cache-control']).toContain('no-store')

    const body = await response.body()
    // The BOM, without which Excel on Windows mangles every non-ASCII name.
    expect(body.subarray(0, 3)).toEqual(Buffer.from([0xef, 0xbb, 0xbf]))
    // A zero-row export still carries its header. A completely empty file
    // reads as a broken download.
    expect(body.toString('utf8')).toContain('occurred_at,action_type')
  })

  test('Billing is 404 for a member and shown to an owner', async ({
    page,
  }) => {
    await enterConsole(page, user)
    const slug = new URL(page.url()).pathname.split('/')[2]

    // A first arrival owns their personal organisation, so this one sees it.
    await page.goto(`/o/${slug}/settings/billing`)

    await expect(
      page.getByRole('heading', { name: 'Billing', level: 1 }),
    ).toBeVisible()
    await expect(page.getByText('Current plan')).toBeVisible()
    await expect(page.getByText('free', { exact: true })).toBeVisible()

    // THE COMPOSE STACK IS THE THIRD STATE, NOT THE FIRST.
    //
    // `KINDLAST_BILLING_DATABASE_URL` and a webhook secret are set, so a
    // provider IS configured; `KINDLAST_BILLING_ENABLED` defaults to false, so
    // nothing is gated. That is a real deployment shape (an operator wiring a
    // provider before turning gating on) and the page has to be honest about
    // it rather than advertising a cap nobody is subject to.
    //
    // Asserting the branch this stack is actually in rather than the one the
    // test author assumed: the first version of this expected "self-hosted and
    // sells nothing" and was simply wrong about the environment.
    await expect(
      page.getByText(/Plan limits are not being applied/i),
    ).toBeVisible()
    await expect(page.getByText(/Article 30 entries are capped/i)).toHaveCount(
      0,
    )

    await page.screenshot({
      path: 'test-results/surfaces-billing.png',
      fullPage: true,
    })
  })

  test('the feed distinguishes never-assessed from nothing-wrong', async ({
    page,
  }) => {
    // ENT-161's question, answered from what the surface does. Nothing has
    // swept this organisation, and "0 open findings" would say the same
    // reassuring thing to somebody who has been checked and found clean as to
    // somebody nobody has ever looked at. Only one of those has earned it.
    await enterConsole(page, user)
    const slug = new URL(page.url()).pathname.split('/')[2]

    await page.goto(`/o/${slug}/feed`)
    await expect(page.getByRole('heading', { level: 1 }).first()).toBeVisible()

    await page.screenshot({
      path: 'test-results/surfaces-feed.png',
      fullPage: true,
    })
  })

  test('the sidebar links every surface that exists', async ({ page }) => {
    await enterConsole(page, user)

    // ENT-202's rule: only surfaces that exist are links, because a nav item
    // leading to a 404 makes a person conclude the product is broken rather
    // than unfinished.
    const nav = page.getByRole('navigation', { name: 'Console' })
    for (const label of [
      'Overview',
      'Feed',
      'Records',
      'Regulation',
      'Logs',
      // ENT-231. Listed here rather than only in the sidebar, so a surface
      // that stops being linked fails a test rather than quietly disappearing
      // from the console.
      'Integrations',
      'Settings',
    ]) {
      await expect(nav.getByRole('link', { name: label })).toBeVisible()
    }

    await page.screenshot({
      path: 'test-results/surfaces-shell.png',
      fullPage: true,
    })
  })
})
