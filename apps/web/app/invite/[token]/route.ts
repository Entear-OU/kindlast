import { NextResponse, type NextRequest } from 'next/server'
import { startAuthorization } from '@/lib/auth/flow'

/**
 * An invitation link.
 *
 * It does not accept the invitation. It carries the token into the pre-auth
 * state and hands off to registration, because the person clicking it has no
 * session yet and quite possibly no account.
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

  try {
    const url = await startAuthorization({
      // Registration rather than sign-in: most people following an invitation
      // do not have an account yet, and an IdP that ignores `prompt=create`
      // shows the sign-in form anyway, which is a worse first impression
      // rather than a broken flow.
      register: true,
      invitationToken: token,
      returnTo: '/dashboard',
    })
    return NextResponse.redirect(url)
  } catch (error) {
    console.error('invite', error)
    return NextResponse.redirect(new URL('/sign-in?error=exchange', origin))
  }
}
