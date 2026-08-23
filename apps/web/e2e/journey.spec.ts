import { test, expect, type Page } from '@playwright/test'
import {
  countOrganisations,
  createVerifiedUser,
  deleteUser,
  FIXTURE_PASSWORD,
  invitationState,
  mintInvitation,
  type FixtureUser,
} from './fixtures/identity'
import { LANDED_IN_ORG, onOrigin, WEB_HOST } from './fixtures/origin'

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
    await page.waitForURL(LANDED_IN_ORG, { timeout: 30_000 })

    const cookies = await page.context().cookies()
    expect(cookies.some((c) => c.name.startsWith('kindlast'))).toBe(true)

    // The page actually rendered as an authenticated one. A redirect landing
    // on the right URL is not the same as a page that could read the session,
    // and it is the second that the acceptance criterion asks for.
    //
    // Onboarding is where a brand-new person lands now (ENT-212), so this is
    // that page rather than the dashboard's `active-org`. The assertion is not
    // weaker for it: this heading is rendered only after the page has taken
    // the session's access token to core-api and had a MEMBERSHIP resolved
    // from it, which is more of the round trip than a testid on the dashboard
    // proved.
    await expect(
      page.getByRole('heading', { name: 'Getting set up', level: 1 }),
    ).toBeVisible()
    await expect(page.getByRole('button', { name: 'Start' })).toBeVisible()

    // The same heading is what the page shows when it CANNOT read the caller's
    // memberships, because `WorkspaceUnavailable` is given the surface's own
    // title rather than a generic one. So the heading alone would pass against
    // a console that had failed to reach core-api at all, and the absence of
    // that panel is the half that makes the assertion mean anything.
    await expect(page.getByTestId('workspace-unavailable')).toHaveCount(0)

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
    await page.waitForURL(LANDED_IN_ORG, { timeout: 30_000 })

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
    await page.waitForURL(LANDED_IN_ORG, { timeout: 30_000 })

    const response = await page.goto('/o/an-organisation-that-is-not-mine')

    expect(response?.status()).toBe(404)
    // Still on the URL that was asked for. If this had become the caller's own
    // organisation, the address bar would say so.
    await expect(page).toHaveURL(/\/o\/an-organisation-that-is-not-mine$/)
    await expect(page.getByTestId('active-org')).toHaveCount(0)
  })

  /**
   * An invitation that cannot be used says so (ENT-267).
   *
   * The refusal itself is PR #227's and is not in doubt: an invitation names
   * an address, and anybody else holding the token is refused so that its
   * actual recipient can still use it. What was in doubt is whether the person
   * refused ever finds out. `/invite/{token}` redirected them to
   * `/workspace?error=invitation`, `/workspace` read no parameters, and they
   * arrived in an organisation of their own having been told nothing at all.
   *
   * Reachable by accident more often than by malice: an inviter opening their
   * own link to see what the recipient will see is exactly this path, and the
   * silence reads as a broken product.
   *
   * Both halves are asserted here, because either alone would be a wrong
   * outcome that looks right. A message with the invitation consumed would
   * mean the real recipient can never join; the invitation intact with no
   * message is the bug this closes.
   */
  test('an invitation addressed to somebody else explains itself, and is not spent', async ({
    page,
  }) => {
    await page.goto('/sign-in')
    await page.getByRole('button', { name: 'Continue', exact: true }).click()
    await signIn(page, user)
    await page.waitForURL(LANDED_IN_ORG, { timeout: 30_000 })

    // Into the caller's own organisation, deliberately. Membership is not what
    // is being refused here, the address is, and minting it somewhere they
    // already belong keeps that the only variable.
    const slug = new URL(page.url()).pathname.split('/')[2]
    const token = await mintInvitation(
      slug,
      `someone-else-${Date.now()}@kindlast.test`,
    )

    await page.goto(`/invite/${token}`)

    await expect(page).toHaveURL(onOrigin('/workspace\\?error=invitation'), {
      timeout: 30_000,
    })

    const message = page.getByTestId('invitation-failed')
    await expect(message).toBeVisible()

    // What the person is told: that the account they are signed in with is the
    // problem, and what to do about it. That is ENT-267's acceptance criterion
    // and it is what makes this different from the silent redirect it replaced.
    await expect(message).toContainText(/account you are signed in with/i)
    await expect(message).toContainText(/sign out and open the link again/i)

    // AND NOT THE ADDRESS ITSELF, WHICH IS WORTH WRITING DOWN (ENT-264).
    //
    // This asserted `toContainText(user.email)` when it was written, because
    // `InvitationFailed` names the signed-in account when it has one and that
    // is the more useful message. It never ran, and against the stack it does
    // not hold: `GetCurrentUser` fills `email` from the OIDC claims, and
    // core-api's own comment in internal/service/session/session.go says the
    // quiet part, that only the provisioning call has a fetched profile and
    // "on every later call these are the token's claims again, so a provider
    // that omits them answers with empty strings". The access token this stack
    // issues omits them, so a returning caller reaches this page with no email
    // to name and the component's documented fallback renders.
    //
    // So the assertion is on the sentence that is always there. Naming the
    // account is a real improvement and the component already supports it; it
    // needs the profile surface core-api's comment defers to, and that is its
    // own piece of work rather than something to weaken this gate over.

    // And nothing else. core-api answers expired, already redeemed, never real
    // and addressed to somebody else identically so that a session cannot be
    // used to discover which invitations exist, and a page that named the
    // cause would hand that distinction straight back.
    for (const oracle of [
      /expired/i,
      /already/i,
      /revoked/i,
      /does not exist/i,
      /not found/i,
      /invalid/i,
    ]) {
      await expect(message).not.toContainText(oracle)
    }

    expect(await invitationState(token)).toBe('unused')
  })

  // Sign-out, driven rather than asserted about.
  //
  // auth.spec.ts already covers the two ways sign-out must refuse: a GET, and
  // a POST with no CSRF token. Both are about requests that should not end a
  // session, and neither of them ever ends one, so the path a person actually
  // takes had no test at all. It was broken the whole time: the seed
  // registered `${origin}/` as the post-logout URI while `endSessionUrl` asks
  // for `${origin}/sign-in`, and an authorization server matches that list
  // exactly.
  //
  // Two assertions, because the visible half is the less important one.
  // Landing back on /sign-in only says the redirect was allowed. What matters
  // is that `end_session` actually ran, and the way to know is to try to sign
  // in again: if the provider still holds the session, "Continue" walks
  // straight back into the workspace without ever asking for a password.
  // That is the state this test exists to make impossible, and the reason
  // RP-initiated logout is in the product at all.
  test('signing out ends the session at the provider, not only here', async ({
    page,
  }) => {
    await page.goto('/sign-in')
    await page.getByRole('button', { name: 'Continue', exact: true }).click()
    await signIn(page, user)
    await page.waitForURL(LANDED_IN_ORG, { timeout: 30_000 })

    await page.getByRole('button', { name: 'Sign out' }).first().click()

    // Back on our own origin. A post-logout URI the provider was not given
    // leaves the browser on the provider's domain showing raw JSON, so this
    // fails on the URL rather than on anything subtle.
    await page.waitForURL(onOrigin('/sign-in'), { timeout: 30_000 })

    // And the provider forgot them. Asking to sign in again must not walk
    // straight back into the workspace, because that is what an authorization
    // server that still holds the session does.
    await page.getByRole('button', { name: 'Continue', exact: true }).click()

    // THE ASSERTION IS "WE ARE STILL AT THE PROVIDER", AND THAT IS THE WHOLE
    // OF IT. If `end_session` had not run, Zitadel would have recognised the
    // session, issued a code without asking anything, and put the browser back
    // on our own origin inside the organisation. Never arriving back here is
    // the property; what the provider chooses to render instead is its own
    // business and not something this repository should be asserting about.
    await page.waitForURL((url) => url.host !== WEB_HOST, { timeout: 20_000 })

    // Then, loosely, that it is asking rather than proceeding.
    //
    // This spelled out a credential input when it was written, and it never
    // ran, so nobody saw what Zitadel actually does: it shows an account
    // picker headed "Select Account", listing the person with the words
    // "Signed out" under their name, and a password field appears only after
    // they choose. The old assertion failed on a stack where sign-out was
    // working perfectly, which is the worst way for a test to be wrong.
    //
    // Either shape counts, because either one means the provider stopped
    // treating them as signed in.
    const credential = page
      .locator(
        'input[name="loginName"], input[type="email"], input[type="password"]',
      )
      .first()
    const signedOut = page.getByText(/signed out/i).first()
    await expect(credential.or(signedOut)).toBeVisible({ timeout: 20_000 })
  })
})
