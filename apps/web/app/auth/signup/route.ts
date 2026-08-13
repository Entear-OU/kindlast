import { NextResponse, type NextRequest } from 'next/server'
import { startAuthorization } from '@/lib/auth/flow'

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
  const { searchParams, origin } = request.nextUrl

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
