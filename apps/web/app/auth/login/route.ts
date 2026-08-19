import { NextResponse, type NextRequest } from 'next/server'
import { startAuthorization } from '@/lib/auth/flow'
import { publicOrigin } from '@/lib/auth/public-origin'

/**
 * Start sign-in: build the authorization request and redirect to it.
 *
 * `?idp=google` adds an `idp_hint`, which is why "Continue with Google" is a
 * parameter rather than a separate flow with its own callback and its own
 * bugs (§1.7).
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
    })
    return NextResponse.redirect(url)
  } catch (error) {
    console.error('auth/login', error)
    return NextResponse.redirect(new URL('/sign-in?error=exchange', origin))
  }
}
