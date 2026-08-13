import { describe, it, expect } from 'vitest'
import { safeReturnTo, DEFAULT_RETURN_TO } from '@/lib/auth/return-to'

/**
 * `returnTo` arrives in a query parameter on /auth/login and is used to build a
 * redirect after a successful sign-in. If a hostile value survives, the result
 * is post-authentication phishing: the person signs in on the real domain,
 * with a real session, and is then handed to a page an attacker controls,
 * which is the ideal moment to imitate a "session expired" prompt.
 *
 * The first version of this guard checked `startsWith('/')` and
 * `!startsWith('//')`, which looks sufficient and is not. Browsers treat a
 * backslash as a forward slash in the authority position, so `/\evil.com`
 * passed and resolved to `http://evil.com/`. Found by a security review, and
 * confirmed against the URL API before being fixed rather than taken on faith.
 *
 * These cases are written as the resolved destination rather than as string
 * shapes, because the string shape is exactly what the broken version got
 * right.
 */

const ORIGIN = 'http://localhost:3000'

/** Where a browser would actually send someone for a given value. */
function resolves(value: string): string {
  return new URL(safeReturnTo(value), ORIGIN).origin
}

describe('safeReturnTo', () => {
  it.each([
    ['/dashboard', '/dashboard'],
    ['/records/ropa', '/records/ropa'],
    ['/feed?filter=open', '/feed?filter=open'],
    ['/feed#latest', '/feed#latest'],
  ])('keeps the same-origin path %s', (input, expected) => {
    expect(safeReturnTo(input)).toBe(expected)
  })

  it.each([
    ['nothing', undefined],
    ['an empty string', ''],
    ['an absolute http url', 'http://evil.com'],
    ['an absolute https url', 'https://evil.com/phish'],
    ['a protocol-relative url', '//evil.com'],
    // The bypass the original guard allowed.
    ['a backslash authority', '/\\evil.com'],
    ['a double backslash', '\\\\evil.com'],
    ['a mixed slash authority', '/\\/evil.com'],
    ['a javascript url', 'javascript:alert(1)'],
    ['a data url', 'data:text/html,<script>alert(1)</script>'],
    ['a relative path', 'dashboard'],
  ])('refuses %s', (_name, input) => {
    expect(safeReturnTo(input as string | undefined)).toBe(DEFAULT_RETURN_TO)
  })

  it.each(['/\\evil.com', '//evil.com', 'https://evil.com', '\\\\evil.com'])(
    'never resolves %s off this origin',
    (input) => {
      // The assertion that actually matters: not what the string looks like,
      // but where a browser would go.
      expect(resolves(input)).toBe(ORIGIN)
    },
  )

  it('strips control characters rather than trusting them', () => {
    // Browsers strip tabs and newlines from URLs before resolving, so a value
    // carrying them is never what it appears to be.
    expect(safeReturnTo('/\tevil.com')).toBe(DEFAULT_RETURN_TO)
    expect(safeReturnTo('/\nevil.com')).toBe(DEFAULT_RETURN_TO)
  })
})
