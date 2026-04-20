import { NextResponse } from 'next/server'

// OAuth callback - currently not supported
// Will be implemented when Gateway OAuth support is added
export async function GET(request: Request) {
  const origin = new URL(request.url).origin

  // OAuth is not yet supported, redirect to login
  return NextResponse.redirect(new URL('/login?error=oauth_not_supported', origin))
}
