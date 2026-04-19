/**
 * Gateway JWT token management.
 *
 * This module bridges Supabase authentication with the backend gateway.
 * It handles:
 * - Exchanging Supabase tokens for gateway JWT tokens
 * - Storing tokens securely in httpOnly cookies
 * - Refreshing tokens before expiry
 * - Providing a helper to get valid tokens for API calls
 */

import { cookies } from 'next/headers';
import { getApiConfig, buildApiUrl, API_ENDPOINTS } from './config';

/**
 * User profile returned from gateway auth endpoints
 */
export interface GatewayUser {
  id: string;
  email: string;
  plan: string;
  full_name?: string;
  created_at?: string;
}

/**
 * Auth response from gateway
 */
export interface GatewayAuthResponse {
  accessToken: string;
  refreshToken: string;
  user: GatewayUser;
}

/**
 * Custom error for gateway authentication failures
 */
export class GatewayAuthError extends Error {
  constructor(
    message: string,
    public readonly code: string,
    public readonly status?: number
  ) {
    super(message);
    this.name = 'GatewayAuthError';
  }
}

/**
 * Parse JWT expiry time from token payload.
 * Does NOT validate the token signature - only extracts the exp claim.
 *
 * @param token JWT token string
 * @returns Date of expiry or null if cannot be parsed
 */
export function parseJwtExpiry(token: string): Date | null {
  if (!token) {
    return null;
  }

  try {
    const parts = token.split('.');
    if (parts.length !== 3) {
      return null;
    }

    // Decode base64url payload
    const payload = parts[1];
    const decoded = Buffer.from(payload, 'base64url').toString('utf-8');
    const claims = JSON.parse(decoded);

    if (typeof claims.exp !== 'number') {
      return null;
    }

    return new Date(claims.exp * 1000);
  } catch {
    return null;
  }
}

/**
 * Check if a JWT token is expired or about to expire.
 *
 * @param token JWT token string
 * @param bufferMs Buffer time in milliseconds (default from config)
 * @returns true if token is expired or will expire within buffer
 */
export function isTokenExpired(token: string, bufferMs?: number): boolean {
  const config = getApiConfig();
  const buffer = bufferMs ?? config.tokenExpiryBuffer;

  const expiry = parseJwtExpiry(token);
  if (!expiry) {
    return true; // Treat unparseable tokens as expired
  }

  const now = Date.now();
  return expiry.getTime() - buffer <= now;
}

/**
 * Exchange a Supabase session token for gateway JWT tokens.
 *
 * @param supabaseToken The Supabase access token
 * @returns Gateway auth response with tokens and user info
 * @throws GatewayAuthError on failure
 */
export async function exchangeSupabaseToken(
  supabaseToken: string
): Promise<GatewayAuthResponse> {
  const config = getApiConfig();
  const url = buildApiUrl(API_ENDPOINTS.auth.exchange, config);

  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ supabase_token: supabaseToken }),
  });

  const data = await response.json();

  if (!response.ok) {
    throw new GatewayAuthError(
      `Failed to exchange token: ${data.error || 'Unknown error'}`,
      data.code || 'EXCHANGE_FAILED',
      response.status
    );
  }

  return {
    accessToken: data.access_token,
    refreshToken: data.refresh_token,
    user: data.user,
  };
}

/**
 * Refresh gateway access token using refresh token.
 *
 * @param refreshToken The gateway refresh token
 * @returns New gateway auth response with fresh tokens
 * @throws GatewayAuthError on failure
 */
export async function refreshGatewayToken(
  refreshToken: string
): Promise<GatewayAuthResponse> {
  const config = getApiConfig();
  const url = buildApiUrl(API_ENDPOINTS.auth.refresh, config);

  const response = await fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ refresh_token: refreshToken }),
  });

  const data = await response.json();

  if (!response.ok) {
    throw new GatewayAuthError(
      `Failed to refresh token: ${data.error || 'Unknown error'}`,
      data.code || 'REFRESH_FAILED',
      response.status
    );
  }

  return {
    accessToken: data.access_token,
    refreshToken: data.refresh_token,
    user: data.user,
  };
}

/**
 * Store gateway tokens in secure httpOnly cookies.
 * Should only be called from server-side code.
 *
 * @param accessToken Gateway access token
 * @param refreshToken Gateway refresh token
 */
export async function storeGatewayTokens(
  accessToken: string,
  refreshToken: string
): Promise<void> {
  const config = getApiConfig();
  const cookieStore = await cookies();

  const isProduction = process.env.NODE_ENV === 'production';

  // Store access token
  cookieStore.set(config.accessTokenCookie, accessToken, {
    httpOnly: true,
    secure: isProduction,
    sameSite: 'lax',
    path: '/',
    // Access token expiry - parse from token or use default
    maxAge: getTokenMaxAge(accessToken, 15 * 60), // Default 15 minutes
  });

  // Store refresh token
  cookieStore.set(config.refreshTokenCookie, refreshToken, {
    httpOnly: true,
    secure: isProduction,
    sameSite: 'lax',
    path: '/',
    // Refresh token typically has longer expiry
    maxAge: getTokenMaxAge(refreshToken, 7 * 24 * 60 * 60), // Default 7 days
  });
}

/**
 * Get max age for cookie from token expiry
 */
function getTokenMaxAge(token: string, defaultSeconds: number): number {
  const expiry = parseJwtExpiry(token);
  if (!expiry) {
    return defaultSeconds;
  }

  const secondsUntilExpiry = Math.floor((expiry.getTime() - Date.now()) / 1000);
  return Math.max(0, secondsUntilExpiry);
}

/**
 * Get gateway access token from cookie.
 *
 * @returns Access token or null if not stored
 */
export async function getGatewayToken(): Promise<string | null> {
  const config = getApiConfig();
  const cookieStore = await cookies();

  const cookie = cookieStore.get(config.accessTokenCookie);
  return cookie?.value ?? null;
}

/**
 * Get gateway refresh token from cookie.
 *
 * @returns Refresh token or null if not stored
 */
export async function getGatewayRefreshToken(): Promise<string | null> {
  const config = getApiConfig();
  const cookieStore = await cookies();

  const cookie = cookieStore.get(config.refreshTokenCookie);
  return cookie?.value ?? null;
}

/**
 * Clear gateway tokens from cookies.
 * Call this on logout or when tokens are invalid.
 */
export async function clearGatewayTokens(): Promise<void> {
  const config = getApiConfig();
  const cookieStore = await cookies();

  cookieStore.delete(config.accessTokenCookie);
  cookieStore.delete(config.refreshTokenCookie);
}

/**
 * Get a valid gateway access token, refreshing if necessary.
 * This is the main helper for making authenticated API calls.
 *
 * @returns Valid access token or null if not authenticated
 */
export async function getValidGatewayToken(): Promise<string | null> {
  const accessToken = await getGatewayToken();

  // No token stored
  if (!accessToken) {
    return null;
  }

  // Token is still valid
  if (!isTokenExpired(accessToken)) {
    return accessToken;
  }

  // Try to refresh
  const refreshToken = await getGatewayRefreshToken();
  if (!refreshToken) {
    return null;
  }

  try {
    const response = await refreshGatewayToken(refreshToken);
    await storeGatewayTokens(response.accessToken, response.refreshToken);
    return response.accessToken;
  } catch {
    // Refresh failed - clear invalid tokens
    await clearGatewayTokens();
    return null;
  }
}

/**
 * Authenticate with gateway using Supabase session.
 * Exchanges Supabase token for gateway tokens and stores them.
 *
 * @param supabaseToken The Supabase access token from session
 * @returns Gateway user info
 * @throws GatewayAuthError on failure
 */
export async function authenticateWithGateway(
  supabaseToken: string
): Promise<GatewayUser> {
  const response = await exchangeSupabaseToken(supabaseToken);
  await storeGatewayTokens(response.accessToken, response.refreshToken);
  return response.user;
}
