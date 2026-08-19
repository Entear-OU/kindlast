/**
 * The origin a browser reaches this console at (ENT-241).
 *
 * WHY NOT `request.nextUrl.origin`, WHICH IS WHAT EVERY HANDLER USED
 *
 * Because it is not the browser's origin once anything sits in front of the
 * app. Next's standalone server composes that value from the address it was
 * told to listen on, so the containerised console, bound to 0.0.0.0:3000
 * behind the edge, answered redirects with `Location: http://0.0.0.0:3000/...`.
 * A browser cannot follow that, and the failure lands one step later than the
 * cause: the sign-in completes, the callback redirects, and the person ends up
 * on a connection error with no clue which service produced it.
 *
 * WHY CONFIGURATION RATHER THAN A FORWARDED HEADER
 *
 * `X-Forwarded-Host` would work behind our own edge and is the conventional
 * answer, but it is a value the request carries, and this function decides
 * where a person is sent next. A deployment that ever exposes the app directly,
 * or a proxy that passes a client-supplied header through, turns that into an
 * open redirect. The registered redirect URI is the one origin an operator has
 * already had to state, the authorization server verifies it, and it cannot be
 * influenced by a caller.
 *
 * The fallback keeps the previous behaviour for any surface reached in a
 * deployment with no redirect URI configured, which is one that cannot sign
 * anybody in anyway.
 */
export function publicOrigin(requestOrigin: string): string {
  const configured = process.env.KINDLAST_WEB_REDIRECT_URI?.trim()
  if (!configured) return requestOrigin

  try {
    return new URL(configured).origin
  } catch {
    // A malformed value is a misconfiguration, and it is reported where it
    // matters: sign-in refuses on it. Falling back here rather than throwing
    // keeps a bad setting from turning every redirect into a 500.
    return requestOrigin
  }
}
