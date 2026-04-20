/**
 * Gateway auth middleware utilities.
 * Checks for JWT tokens in cookies and validates authentication.
 */

import { NextResponse, type NextRequest } from 'next/server'
import { getApiConfig, API_ENDPOINTS, buildApiUrl } from '@/lib/api/config'

export interface AuthUser {
  id: string
  email: string
  plan: string
  full_name?: string
}

/**
 * Parse JWT expiry time from token payload without verifying signature.
 * For middleware use only - actual verification happens on Gateway.
 */
function parseJwtExpiry(token: string): Date | null {
  try {
    const parts = token.split('.')
    if (parts.length !== 3) return null

    // Base64url decode the payload
    const payload = parts[1]
    const decoded = atob(payload.replace(/-/g, '+').replace(/_/g, '/'))
    const claims = JSON.parse(decoded)

    if (typeof claims.exp !== 'number') return null
    return new Date(claims.exp * 1000)
  } catch {
    return null
  }
}

/**
 * Check if token is expired with buffer.
 */
function isTokenExpired(token: string, bufferMs: number = 60000): boolean {
  const expiry = parseJwtExpiry(token)
  if (!expiry) return true
  return expiry.getTime() - bufferMs <= Date.now()
}

/**
 * Refresh the access token using the refresh token.
 */
async function refreshToken(refreshToken: string): Promise<{
  accessToken: string
  refreshToken: string
} | null> {
  const config = getApiConfig()
  const url = buildApiUrl(API_ENDPOINTS.auth.refresh, config)

  try {
    const response = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
    })

    if (!response.ok) return null

    const data = await response.json()
    return {
      accessToken: data.access_token,
      refreshToken: data.refresh_token,
    }
  } catch {
    return null
  }
}

/**
 * Check auth status from cookies and refresh if needed.
 * Returns the user and a potentially modified response with updated cookies.
 */
export async function checkAuth(request: NextRequest): Promise<{
  response: NextResponse
  user: AuthUser | null
  accessToken: string | null
}> {
  const config = getApiConfig()
  let response = NextResponse.next({ request })

  // Get tokens from cookies
  const accessToken = request.cookies.get(config.accessTokenCookie)?.value
  const refreshTokenValue = request.cookies.get(config.refreshTokenCookie)?.value

  // No tokens at all - not authenticated
  if (!accessToken && !refreshTokenValue) {
    return { response, user: null, accessToken: null }
  }

  // Check if access token is valid
  if (accessToken && !isTokenExpired(accessToken)) {
    // Token is valid, try to get user info
    try {
      const url = buildApiUrl(API_ENDPOINTS.auth.me, config)
      const userResponse = await fetch(url, {
        headers: { 'Authorization': `Bearer ${accessToken}` },
      })

      if (userResponse.ok) {
        const user = await userResponse.json() as AuthUser
        return { response, user, accessToken }
      }
    } catch {
      // Token might be invalid on server side, try to refresh
    }
  }

  // Try to refresh the token
  if (refreshTokenValue) {
    const tokens = await refreshToken(refreshTokenValue)

    if (tokens) {
      // Set new cookies
      const isProduction = process.env.NODE_ENV === 'production'

      response.cookies.set(config.accessTokenCookie, tokens.accessToken, {
        httpOnly: true,
        secure: isProduction,
        sameSite: 'lax',
        path: '/',
        maxAge: 15 * 60, // 15 minutes
      })

      response.cookies.set(config.refreshTokenCookie, tokens.refreshToken, {
        httpOnly: true,
        secure: isProduction,
        sameSite: 'lax',
        path: '/',
        maxAge: 7 * 24 * 60 * 60, // 7 days
      })

      // Get user info with new token
      try {
        const url = buildApiUrl(API_ENDPOINTS.auth.me, config)
        const userResponse = await fetch(url, {
          headers: { 'Authorization': `Bearer ${tokens.accessToken}` },
        })

        if (userResponse.ok) {
          const user = await userResponse.json() as AuthUser
          return { response, user, accessToken: tokens.accessToken }
        }
      } catch {
        // Failed to get user, clear cookies
      }
    }

    // Refresh failed, clear cookies
    response.cookies.delete(config.accessTokenCookie)
    response.cookies.delete(config.refreshTokenCookie)
  }

  return { response, user: null, accessToken: null }
}

/**
 * Protected routes that require authentication.
 */
const PROTECTED_ROUTES = [
  '/dashboard',
  '/onboarding',
  '/settings',
  '/copilot',
]

/**
 * Public routes that should redirect to dashboard if already logged in.
 */
const AUTH_ROUTES = [
  '/login',
  '/signup',
  '/register',
]

/**
 * Middleware to handle auth routing.
 */
export async function authMiddleware(request: NextRequest): Promise<NextResponse> {
  const { pathname } = request.nextUrl
  const { response, user } = await checkAuth(request)

  // Check if route is protected
  const isProtectedRoute = PROTECTED_ROUTES.some(route => pathname.startsWith(route))
  const isAuthRoute = AUTH_ROUTES.some(route => pathname.startsWith(route))

  // Redirect to login if accessing protected route without auth
  if (isProtectedRoute && !user) {
    const loginUrl = new URL('/login', request.url)
    loginUrl.searchParams.set('redirect', pathname)
    return NextResponse.redirect(loginUrl)
  }

  // Redirect to dashboard if accessing auth routes while logged in
  if (isAuthRoute && user) {
    return NextResponse.redirect(new URL('/dashboard', request.url))
  }

  return response
}
