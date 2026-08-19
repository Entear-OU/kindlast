import { NextResponse, type NextRequest } from 'next/server'
import { startAuthorization } from '@/lib/auth/flow'
import { publicOrigin } from '@/lib/auth/public-origin'

/**
 * Start registration.
 *
 * Identical to /auth/login plus `prompt=create`. There is no signup endpoint
 * of ours behind this, and that is the point: the registration form, the
 * password rules, email verification, lockout and MFA all belong to the
 * authorization server, so this system has no user-enumeration surface and no
 * password handling to review (§1.7).
 */
export async function GET(request: NextRequest) {
  const { searchParams, origin: servedFrom } = request.nextUrl
  // The origin a browser can reach, not the one this process is listening
  // on. Behind the edge those differ, and a redirect to the second is a
  // dead end (ENT-241). See lib/auth/public-origin.ts.
  const origin = publicOrigin(servedFrom)

  try {
    const url = await startAuthorization({
      returnTo: searchParams.get('returnTo') ?? undefined,
      idp: searchParams.get('idp'),
      register: true,
    })
    return NextResponse.redirect(url)
  } catch (error) {
    console.error('auth/signup', error)
    return NextResponse.redirect(new URL('/sign-in?error=exchange', origin))
  }
}
