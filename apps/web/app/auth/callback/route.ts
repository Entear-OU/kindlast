import { NextResponse, type NextRequest } from 'next/server'
import { exchangeCode, safeReturnTo } from '@/lib/auth/flow'
import { consumeState } from '@/lib/auth/state'
import {
  createSession,
  sessionCookieOptions,
  SESSION_COOKIE,
} from '@/lib/auth/session'
import { subjectOf } from '@/lib/auth/claims'
import {
  acceptInvitation,
  activeOrgFrom,
  getCurrentUser,
} from '@/lib/auth/client'
import { publicOrigin } from '@/lib/auth/public-origin'

/**
 * The other end of the authorization request.
 *
 * This replaced the Supabase code exchange that lived here. Kindlast is being
 * moved off Supabase, and the auth path goes first because it is the one part
 * with a finished replacement: core-api verifies tokens and serves
 * /api/v1/me, so nothing here needs Supabase any more.
 *
 * Order matters and is worth reading. State is verified and consumed before
 * anything else happens, because it is the only thing tying this callback to a
 * request this server actually started. Then the code is exchanged on the back
 * channel. Only then does a session exist.
 */
export async function GET(request: NextRequest) {
  const { searchParams, origin: servedFrom } = request.nextUrl
  // The origin a browser can reach, not the one this process is listening
  // on. Behind the edge those differ, and a redirect to the second is a
  // dead end (ENT-241). See lib/auth/public-origin.ts.
  const origin = publicOrigin(servedFrom)

  // The user declined at the IdP, or the IdP refused. Not an error worth a
  // stack trace: it is a person changing their mind.
  const denied = searchParams.get('error')
  if (denied) {
    return NextResponse.redirect(new URL('/sign-in?error=denied', origin))
  }

  const code = searchParams.get('code')
  const state = searchParams.get('state')
  if (!code || !state) {
    return NextResponse.redirect(new URL('/sign-in?error=state', origin))
  }

  // Single use, and atomic: two concurrent callbacks cannot both succeed.
  // An unknown or expired state is indistinguishable from a forged one here,
  // which is the correct amount of information to act on.
  const preAuth = await consumeState(state)
  if (!preAuth) {
    return NextResponse.redirect(new URL('/sign-in?error=state', origin))
  }

  let tokens
  try {
    tokens = await exchangeCode(code, preAuth.verifier)
  } catch (error) {
    console.error('auth/callback exchange', error)
    return NextResponse.redirect(new URL('/sign-in?error=exchange', origin))
  }

  const subject = subjectOf(tokens.accessToken)
  if (!subject) {
    console.error('auth/callback: access token carries no subject')
    return NextResponse.redirect(new URL('/sign-in?error=exchange', origin))
  }

  // The invitation is redeemed before the first GetCurrentUser, and the
  // ordering is not incidental: get it backwards and just-in-time
  // provisioning sees a subject with no membership and creates a personal
  // organisation alongside the one they were invited to (§1.8).
  //
  // Best effort, both of them. A failure here must not strand someone holding
  // a valid token on an error page: they are signed in either way, and both
  // calls are idempotent on the subject, so the next navigation can retry.
  //
  // Best effort is not the same as unremarked, which is what it used to be
  // (ENT-267). A new person whose token failed here is signed in, holds a
  // personal organisation they never asked for, and has nothing telling them
  // the invitation is why the company that invited them is nowhere to be seen.
  // That was the shape of the PR #230 symptom: fixed at its cause, and still
  // silent if it ever recurs.
  let invitationFailed = false
  if (preAuth.invitationToken) {
    const joined = await acceptInvitation(
      tokens.accessToken,
      preAuth.invitationToken,
    )
    // `orgSlug` rather than the response being present, matching the invite
    // route: the redirect is built from the slug, so a response without one is
    // no more usable than no response at all.
    invitationFailed = !joined?.orgSlug
  }

  // The bootstrap. For a first-time arrival this is the call that creates the
  // organisation, so it has to happen before the session is written or there
  // is nothing to name in the active-organisation header.
  const me = await getCurrentUser(tokens.accessToken)

  const sessionId = await createSession({
    accessToken: tokens.accessToken,
    refreshToken: tokens.refreshToken,
    idToken: tokens.idToken,
    expiresAt: tokens.expiresAt,
    subject,
    orgId: activeOrgFrom(me),
  })

  // A failed invitation overrides the destination rather than decorating it.
  // `/workspace` is the only path that renders the explanation, and it is
  // where this flow was going anyway: /invite/{token} deliberately sets no
  // returnTo, because the organisation being joined has no known URL until the
  // redemption that just failed. Appending the parameter to some other
  // destination would set it somewhere nothing reads it, which is the bug this
  // is fixing.
  const response = NextResponse.redirect(
    new URL(
      invitationFailed
        ? '/workspace?error=invitation'
        : safeReturnTo(preAuth.returnTo),
      origin,
    ),
  )
  response.cookies.set(SESSION_COOKIE, sessionId, sessionCookieOptions())
  return response
}
