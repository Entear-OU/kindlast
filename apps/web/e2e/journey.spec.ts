import { test, expect, type Page } from '@playwright/test'
import {
  countOrganisations,
  createVerifiedUser,
  deleteUser,
  FIXTURE_PASSWORD,
  type FixtureUser,
} from './fixtures/identity'

/**
 * The whole round trip, which is the part nothing else covers.
 *
 * auth.spec.ts proves we leave for the identity provider correctly and that
 * the signed-out surfaces behave. It stops at the hand-off, so everything
 * after the redirect comes back has until now been asserted only against
 * fakes: the code exchange, the session cookie, and the provisioning call
 * that gives a brand-new person their first organisation.
 *
 * That last one is why this test earns its runtime. §1.8's ordering and the
 * `openid` decision both exist to make one thing possible, a caller who holds
 * nothing reaching the endpoint that grants them something, and a bootstrap
 * that cannot start is invisible to every test that starts from a seeded
 * session.
 *
 * Needs the compose stack:
 *
 *     docker compose -f deploy/compose.yaml up -d
 */

/**
 * Zitadel offers to enrol a second factor after a first password sign-in. It
 * is an offer rather than a requirement under this stack's login policy, and
 * enrolling one is Zitadel's behaviour to test rather than ours.
 *
 * Raced against arriving back on our own origin instead of simply waited for,
 * so that a policy change which stops prompting does not cost this test a
 * fixed timeout on every run.
 */
async function skipSecondFactorOffer(page: Page) {
  const skip = page.locator('[name="skip"]').first()

  const outcome = await Promise.race([
    page
      .waitForURL(/localhost:3000/, { timeout: 20_000 })
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
 * Drive Zitadel's hosted login. The selectors are deliberately loose: this is
 * a screen we do not own and do not control the markup of, and a test that
 * breaks on someone else's class name is a maintenance tax with no coverage
 * attached.
 */
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

test.describe('the signup journey', () => {
  let user: FixtureUser

  test.beforeAll(async () => {
    user = await createVerifiedUser('journey')
  })

  test.afterAll(async () => {
    if (user) await deleteUser(user.id)
  })

  // Serial: the second test asserts what the first one's arrival created, so
  // running them independently would assert nothing.
  test.describe.configure({ mode: 'serial' })

  test('a person who has never signed in lands in an organisation of their own', async ({
    page,
  }) => {
    const before = await countOrganisations()

    await page.goto('/sign-in')
    await page.getByRole('button', { name: 'Continue', exact: true }).click()

    await signIn(page, user)

    // Back on our origin, past the callback, and resolved through /workspace
    // into the organisation's own URL. The proxy would have bounced us to
    // /sign-in without a session that survived the navigation.
    await page.waitForURL(/localhost:3000\/o\/[a-z0-9-]+$/, { timeout: 30_000 })

    const cookies = await page.context().cookies()
    expect(cookies.some((c) => c.name.startsWith('kindlast'))).toBe(true)

    // The page actually rendered as an authenticated one. A redirect landing
    // on the right URL is not the same as a page that could read the session,
    // and it is the second that the acceptance criterion asks for.
    await expect(page.getByTestId('active-org')).toBeVisible()

    // The bootstrap actually happened. Without this the test would pass on a
    // session cookie alone, which is the half that was never in doubt.
    expect(await countOrganisations()).toBe(before + 1)
  })

  test('signing in again finds that organisation rather than making another', async ({
    browser,
  }) => {
    // Provisioning is idempotent on the subject, and a second arrival is what
    // proves it. The count is the assertion: reaching the workspace twice
    // would look identical whether one organisation existed or two did.
    const before = await countOrganisations()

    const context = await browser.newContext()
    const page = await context.newPage()

    await page.goto('/workspace')
    await expect(page).toHaveURL(/\/sign-in/)

    await page.getByRole('button', { name: 'Continue', exact: true }).click()
    await signIn(page, user)
    await page.waitForURL(/localhost:3000\/o\/[a-z0-9-]+$/, { timeout: 30_000 })

    expect(await countOrganisations()).toBe(before)

    await context.close()
  })

  // The ENT-198 criterion that is a tenancy property rather than a routing
  // one: a slug the caller does not belong to is 404, not 403, and above all
  // not a quiet redirect into an organisation they DO belong to.
  //
  // A redirect would be the comfortable thing to build and the wrong thing to
  // ship. It changes what a URL means underneath whoever bookmarked it, which
  // in a product where links carry approval decisions means someone signing
  // off against a company they did not open.
  test('an organisation the caller does not belong to is not found, and does not redirect', async ({
    page,
  }) => {
    await page.goto('/sign-in')
    await page.getByRole('button', { name: 'Continue', exact: true }).click()
    await signIn(page, user)
    await page.waitForURL(/localhost:3000\/o\/[a-z0-9-]+$/, { timeout: 30_000 })

    const response = await page.goto('/o/an-organisation-that-is-not-mine')

    expect(response?.status()).toBe(404)
    // Still on the URL that was asked for. If this had become the caller's own
    // organisation, the address bar would say so.
    await expect(page).toHaveURL(/\/o\/an-organisation-that-is-not-mine$/)
    await expect(page.getByTestId('active-org')).toHaveCount(0)
  })
})
