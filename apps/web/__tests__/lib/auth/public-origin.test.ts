import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { publicOrigin } from '@/lib/auth/public-origin'

/**
 * Where a redirect sends the browser (ENT-241).
 *
 * THE BUG THIS EXISTS FOR
 *
 * Route handlers built their redirects from `request.nextUrl.origin`. Behind a
 * reverse proxy that is not the browser's origin: Next's standalone server
 * composes it from the address it was told to listen on, so a console
 * containerised with HOSTNAME=0.0.0.0 and PORT=3000 answered every redirect
 * with `Location: http://0.0.0.0:3000/...`, which no browser can follow.
 *
 * Measured, not theorised: `/auth/callback?error=access_denied` through the
 * edge returned exactly that, and the end-to-end sign-in failed with the
 * browser sitting on `chrome-error://chromewebdata/`.
 *
 * So the origin comes from configuration rather than from the request. It is
 * the origin of KINDLAST_WEB_REDIRECT_URI, which is the address registered
 * with the authorization server and therefore the one address this console is
 * definitively reachable at. Taking it from a header instead would let a
 * request influence where the next person is sent.
 */
describe('publicOrigin', () => {
  const saved = { ...process.env }

  beforeEach(() => {
    delete process.env.KINDLAST_WEB_REDIRECT_URI
  })

  afterEach(() => {
    process.env = { ...saved }
  })

  it('is the origin of the registered redirect URI', () => {
    process.env.KINDLAST_WEB_REDIRECT_URI =
      'http://localhost:8000/auth/callback'

    expect(publicOrigin('http://0.0.0.0:3000')).toBe('http://localhost:8000')
  })

  it('keeps a non-default port and a real hostname', () => {
    process.env.KINDLAST_WEB_REDIRECT_URI =
      'https://compliance.example.com:8443/auth/callback'

    expect(publicOrigin('http://0.0.0.0:3000')).toBe(
      'https://compliance.example.com:8443',
    )
  })

  it('drops the path, so nothing is appended to /auth/callback', () => {
    process.env.KINDLAST_WEB_REDIRECT_URI =
      'https://compliance.example/auth/callback?x=1'

    expect(publicOrigin('http://0.0.0.0:3000')).toBe(
      'https://compliance.example',
    )
  })

  it('falls back to the request origin when nothing is configured', () => {
    // A deployment with no redirect URI cannot sign anybody in, so this path
    // is only ever reached by a surface that does not need one. Behaving as
    // before is the right answer there rather than throwing.
    expect(publicOrigin('http://localhost:3000')).toBe('http://localhost:3000')
  })

  it('falls back rather than throwing on a value that is not a URL', () => {
    process.env.KINDLAST_WEB_REDIRECT_URI = 'not-a-url'

    expect(publicOrigin('http://localhost:3000')).toBe('http://localhost:3000')
  })
})
