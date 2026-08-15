import { NextResponse, type NextRequest } from 'next/server'
import { startAuthorization } from '@/lib/auth/flow'
import { acceptInvitation } from '@/lib/auth/client'
import { orgPath } from '@/lib/auth/org'
import { currentSession } from '@/lib/auth/session'

/**
 * An invitation link.
 *
 * Two branches, because two quite different people click this link.
 *
 * SIGNED OUT, which is the common case: the token is carried into the pre-auth
 * state and handed off to registration. The invitation is not accepted here.
 *
 * SIGNED IN, which happens more than the first design assumed: a consultant
 * already working in one client's organisation is invited to another, or
 * somebody follows the link in a browser where they never signed out. Sending
 * them through registration would ask an authenticated person to create an
 * account, and an IdP that honours `prompt=create` would show them a signup
 * form for an identity they already have. So the token is redeemed
 * immediately and they land inside the organisation they just joined.
 *
 * The ordering this exists to guarantee (§1.8): the token must survive the
 * round trip to the authorization server so the callback can redeem it
 * **before** the first /api/v1/me. Get it the other way round and provisioning
 * sees a subject with no membership, creates a personal organisation, and the
 * invited user ends up owning one they never asked for alongside the one they
 * were invited to.
 *
 * Nothing is validated here, deliberately. Only core-api can say whether a
 * token is real, since only core-api can see the hashed value, and asking it
 * before the person has authenticated would mean an unauthenticated endpoint
 * that reports whether a given invitation exists. That is an oracle, and this
 * route would be the thing serving it. An invalid token simply fails at the
 * point of redemption, where the caller is known.
 */
export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ token: string }> },
) {
  const { token } = await params
  const { origin } = request.nextUrl

  if (!token) {
    return NextResponse.redirect(new URL('/sign-in', origin))
  }

  // The signed-in branch. Redeem now, and land them where they just joined.
  //
  // `orgSlug` comes off the response rather than being looked up afterwards,
  // which is why ENT-198 put it there: the alternative is a second call to
  // discover the URL of an organisation we were just told about.
  //
  // A failure here does NOT fall through to the handoff below. That was the
  // first shape of this branch and it was wrong: the handoff asks the
  // authorization server to register an account, so a signed-in person whose
  // token had merely expired would be shown a signup form for an identity they
  // already have. Better to leave them where they already belong and say so.
  //
  // Expired, already redeemed and never real are indistinguishable here, by
  // design: core-api answers all three alike so this cannot be used to discover
  // which tokens exist. `/workspace` resolves them into an organisation they do
  // have.
  const session = await currentSession()
  if (session) {
    const joined = await acceptInvitation(session.accessToken, token)
    if (joined?.orgSlug) {
      return NextResponse.redirect(new URL(orgPath(joined.orgSlug), origin))
    }
    return NextResponse.redirect(new URL('/workspace?error=invitation', origin))
  }

  try {
    const url = await startAuthorization({
      // Registration rather than sign-in: most people following an invitation
      // do not have an account yet, and an IdP that ignores `prompt=create`
      // shows the sign-in form anyway, which is a worse first impression
      // rather than a broken flow.
      register: true,
      invitationToken: token,
      // No destination, deliberately. The organisation this invitation joins
      // is not known until the callback has redeemed it, and its URL is built
      // from a slug that only exists after that. `/workspace` (the default)
      // resolves the caller into it once there is something to resolve.
      //
      // This used to name `/dashboard`, which the Supabase removal deleted:
      // accepting an invitation ended on a 404, at the one moment a new user
      // has no idea whether the product works.
    })
    return NextResponse.redirect(url)
  } catch (error) {
    console.error('invite', error)
    return NextResponse.redirect(new URL('/sign-in?error=exchange', origin))
  }
}
