import { NextResponse, type NextRequest } from 'next/server'
import { startAuthorization } from '@/lib/auth/flow'

/**
 * Start sign-in: build the authorization request and redirect to it.
 *
 * `?idp=google` adds an `idp_hint`, which is why "Continue with Google" is a
 * parameter rather than a separate flow with its own callback and its own
 * bugs (§1.7).
 */
export async function GET(request: NextRequest) {
  const { searchParams, origin } = request.nextUrl

  try {
    const url = await startAuthorization({
      returnTo: searchParams.get('returnTo') ?? undefined,
      idp: searchParams.get('idp'),
    })
    return NextResponse.redirect(url)
  } catch (error) {
    console.error('auth/login', error)
    return NextResponse.redirect(new URL('/sign-in?error=exchange', origin))
  }
}
