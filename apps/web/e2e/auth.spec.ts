import { test, expect } from '@playwright/test'

/**
 * The auth flow, driven through a real browser.
 *
 * These cover what unit tests structurally cannot: a redirect to a login form
 * this application does not own, a cookie set on the way back, and a session
 * surviving a navigation. The same reasoning §13.2 applies to token
 * verification, one layer up.
 *
 * Needs the compose stack for Zitadel:
 *
 *     docker compose -f deploy/compose.yaml up -d
 */

test.describe('sign-in surface', () => {
  test('offers a hand-off and never a password field', async ({ page }) => {
    await page.goto('/sign-in')

    await expect(
      page.getByRole('heading', { name: 'Sign in', level: 1 }),
    ).toBeVisible()
    await expect(
      page.getByRole('button', { name: 'Continue', exact: true }),
    ).toBeVisible()

    // The claim the page makes about itself has to be true of the page. A
    // password field here would mean this application had started handling
    // credentials, which §1.7 says it never does.
    await expect(page.locator('input[type="password"]')).toHaveCount(0)
    await expect(
      page.getByText('Kindlast never receives your password'),
    ).toBeVisible()
  })

  test('the card is actually visible, not merely present', async ({ page }) => {
    // This exists because the first version of this screen shipped blank: the
    // elements were in the DOM, correctly named and sized, at opacity 0. A
    // test asserting presence would have passed. Opacity is the property that
    // catches it.
    await page.goto('/sign-in')

    const card = page.locator('[data-document]')
    await expect(card).toBeVisible()
    await expect(card).toHaveCSS('opacity', '1')

    for (const name of ['Continue', 'Create an account']) {
      await expect(page.getByRole('button', { name, exact: true })).toHaveCSS(
        'opacity',
        '1',
      )
    }
  })
})

test.describe('hand-off to the identity provider', () => {
  test('starts an authorization code flow with PKCE', async ({ page }) => {
    await page.goto('/sign-in')
    await page.getByRole('button', { name: 'Continue', exact: true }).click()

    // Lands on the authorization server, not on anything of ours.
    await page.waitForURL(/\/oauth\/v2\/authorize|\/ui\/v2\/login|\/login/, {
      timeout: 15_000,
    })

    const url = new URL(page.url())
    const authorize = url.searchParams

    // When Zitadel has already redirected on to its login UI the query is
    // gone, and the assertion that matters is simply that we left our origin.
    if (authorize.has('code_challenge')) {
      expect(authorize.get('code_challenge_method')).toBe('S256')
      expect(authorize.get('response_type')).toBe('code')
      expect(authorize.get('state')).toBeTruthy()
      // The verifier stays on the server. Anything resembling it in the URL
      // would defeat the whole mechanism.
      expect(authorize.has('code_verifier')).toBe(false)
    }

    expect(url.host).not.toBe('localhost:3000')
  })
})

test.describe('sign-out', () => {
  test('refuses a GET, so a prefetched link cannot end a session', async ({
    request,
  }) => {
    // The bug class this guards is the one in §1.7: link prefetchers, mail
    // scanners and security appliances all issue GETs.
    const response = await request.get('/auth/logout', { maxRedirects: 0 })

    expect(response.status()).toBe(405)
  })

  test('refuses a POST without a CSRF token', async ({ request }) => {
    const response = await request.post('/auth/logout', {
      form: {},
      maxRedirects: 0,
    })

    expect(response.status()).toBe(403)
  })
})

test.describe('protected routes', () => {
  test('sends a signed-out visitor to sign-in, remembering where they were going', async ({
    page,
  }) => {
    await page.goto('/workspace')

    await expect(page).toHaveURL(/\/sign-in\?returnTo=%2Fworkspace/)
  })
})
