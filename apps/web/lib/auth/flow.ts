/**
 * Building the authorization request, and exchanging what comes back.
 *
 * Shared by /auth/login and /auth/signup, which differ by one parameter, and
 * by /auth/callback, which is the other end of the same conversation.
 */
import {
  browserAuthorizationEndpoint,
  discoverProvider,
  requireEnv,
} from './oidc'
import { clientCredentials } from './client-credentials'
import { createPkce, randomToken } from './pkce'
import { safeReturnTo } from './return-to'
import { stashState } from './state'

/**
 * The scopes `web` asks for.
 *
 * `openid` is what SessionService.GetCurrentUser declares, so without it the
 * very first call a signed-in page makes is refused at the scope interceptor.
 *
 * THE LAST ONE IS NOT LIKE THE OTHERS, AND IT IS THE REASON THE CONSOLE WORKS
 *
 * `urn:zitadel:iam:org:projects:roles` is a reserved Zitadel scope, not a
 * permission. It asks the authorization server to put the roles this person has
 * been granted into the token, as
 * `urn:zitadel:iam:org:project:{projectId}:roles`, which is the claim core-api
 * reads its scopes from.
 *
 * Without it a token carries no roles at all, however many the person has been
 * granted, and every endpoint declaring a real scope answers 403 (ENT-221).
 * Measured rather than assumed: minting tokens with and without it is the only
 * difference between a roles claim and no roles claim, and the project's
 * `projectRoleAssertion` setting being on is necessary but not sufficient.
 *
 * Note what this file therefore does NOT list. `findings:read`, `org:manage`
 * and the rest are never requested here. They are not OIDC scopes in this
 * architecture; they are project roles a person holds, and asking for them by
 * name achieves nothing. An earlier comment here said the vocabulary "arrives
 * as the endpoints that need it ship", which described a mechanism that does
 * not exist and sent two people looking in the wrong place.
 *
 * The plural in `projects` is not a typo. `urn:zitadel:iam:org:project:{id}:roles`
 * as a requested scope produces no claim; only the plural form does.
 *
 * STILL REQUESTED AFTER CLIENT-CLASS RESOLUTION, AND NOT REDUNDANTLY
 *
 * core-api now derives the human scope set from the token's client rather than
 * from granted roles (ENT-221), which makes the roles claim unread on the human
 * path. It is kept because that resolution is configuration: with
 * KINDLAST_HUMAN_CLIENT_ID unset, core-api falls back to reading granted
 * scopes, and a deployment in that state needs this scope for a human to reach
 * anything at all.
 *
 * So this is the fallback path rather than dead weight. Removing it would make
 * the fallback silently non-functional, which is the same class of bug as the
 * one that made this line necessary in the first place.
 */
const SCOPES = [
  'openid',
  'profile',
  'email',
  'offline_access',
  'urn:zitadel:iam:org:projects:roles',
]

export interface StartOptions {
  /** Where to send the person once they are signed in. */
  returnTo?: string
  /** `prompt=create`, the OIDC hint that the IdP should show registration. */
  register?: boolean
  /** An `idp_hint`, so "Continue with Google" is one parameter, not a flow. */
  idp?: string | null
  /** An invitation token, carried through the round trip (§1.8). */
  invitationToken?: string
}

/**
 * Builds the authorization URL and stashes everything the callback will need.
 *
 * The verifier goes to Redis rather than to a cookie, so it never reaches the
 * browser. That is the whole point of PKCE: an authorization code intercepted
 * in the redirect is useless without it.
 */
export async function startAuthorization(
  options: StartOptions,
): Promise<string> {
  const provider = await discoverProvider()
  const pkce = await createPkce()
  const state = randomToken(32)

  await stashState(state, {
    verifier: pkce.verifier,
    returnTo: safeReturnTo(options.returnTo),
    invitationToken: options.invitationToken,
    createdAt: Date.now(),
  })

  const url = new URL(browserAuthorizationEndpoint(provider))
  url.searchParams.set('client_id', clientCredentials().clientId)
  url.searchParams.set('redirect_uri', redirectUri())
  url.searchParams.set('response_type', 'code')
  url.searchParams.set('scope', SCOPES.join(' '))
  url.searchParams.set('state', state)
  url.searchParams.set('code_challenge', pkce.challenge)
  url.searchParams.set('code_challenge_method', pkce.method)

  if (options.register) {
    // OIDC "Initiating User Registration". Support is uneven across providers,
    // so it is a hint rather than a contract: an IdP that ignores it shows the
    // sign-in form, which is a worse first impression and not a broken flow.
    url.searchParams.set('prompt', 'create')
  }
  if (options.idp) {
    url.searchParams.set('idp_hint', options.idp)
  }

  return url.toString()
}

export interface TokenSet {
  accessToken: string
  refreshToken: string | null
  idToken: string | null
  expiresAt: number
}

/**
 * Exchanges the authorization code on the back channel.
 *
 * Back channel means this server talks to the token endpoint directly, with
 * the client secret, and the browser is not involved. The secret never leaves
 * this process, which is what makes `web` a confidential client rather than a
 * public one (§1.2).
 */
export async function exchangeCode(
  code: string,
  verifier: string,
): Promise<TokenSet> {
  const provider = await discoverProvider()

  const body = new URLSearchParams({
    grant_type: 'authorization_code',
    code,
    redirect_uri: redirectUri(),
    code_verifier: verifier,
    client_id: clientCredentials().clientId,
    client_secret: clientCredentials().clientSecret,
  })

  const response = await fetch(provider.tokenEndpoint, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/x-www-form-urlencoded',
      ...hostHeader(),
    },
    body,
    cache: 'no-store',
  })

  if (!response.ok) {
    // The body can contain the client secret's error context; log the status
    // and nothing else.
    throw new Error(`Token exchange failed with ${response.status}`)
  }

  const token = (await response.json()) as {
    access_token?: string
    refresh_token?: string
    id_token?: string
    expires_in?: number
  }

  if (!token.access_token) {
    throw new Error('Token endpoint returned no access token')
  }

  return {
    accessToken: token.access_token,
    refreshToken: token.refresh_token ?? null,
    idToken: token.id_token ?? null,
    // Thirty seconds of slack, so a token is refreshed just before it expires
    // rather than just after, which would surface as a 401 to the user.
    expiresAt: Math.floor(Date.now() / 1000) + (token.expires_in ?? 600) - 30,
  }
}

/**
 * Spends the refresh token for a new access token.
 *
 * The session outlives the access token deliberately (§1.2): the cookie is
 * good for thirty days, the token for minutes or hours. Without this, that
 * difference is not a feature but a bug on a delay, because a person who comes
 * back the next day holds a session this server accepts and a token core-api
 * refuses.
 *
 * Same back channel as the code exchange, for the same reason: the client
 * secret goes from this process to the token endpoint and the browser is never
 * involved.
 */
export async function refreshTokens(refreshToken: string): Promise<TokenSet> {
  const provider = await discoverProvider()

  const body = new URLSearchParams({
    grant_type: 'refresh_token',
    refresh_token: refreshToken,
    client_id: clientCredentials().clientId,
    client_secret: clientCredentials().clientSecret,
  })

  const response = await fetch(provider.tokenEndpoint, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/x-www-form-urlencoded',
      ...hostHeader(),
    },
    body,
    cache: 'no-store',
  })

  if (!response.ok) {
    // Status only. The body carries client context, and this one is reached
    // on every expiry, so it would be the noisiest place to leak it.
    throw new Error(`Token refresh failed with ${response.status}`)
  }

  const token = (await response.json()) as {
    access_token?: string
    refresh_token?: string
    id_token?: string
    expires_in?: number
  }

  if (!token.access_token) {
    throw new Error('Token endpoint returned no access token')
  }

  return {
    accessToken: token.access_token,
    // Null means "the server did not rotate", not "it is gone". Servers differ,
    // and the caller keeps what it already holds.
    refreshToken: token.refresh_token ?? null,
    idToken: token.id_token ?? null,
    expiresAt: Math.floor(Date.now() / 1000) + (token.expires_in ?? 600) - 30,
  }
}

/** Revokes a refresh token at sign-out, so it cannot be used again. */
export async function revokeToken(token: string): Promise<void> {
  const provider = await discoverProvider()
  if (!provider.revocationEndpoint) return

  await fetch(provider.revocationEndpoint, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/x-www-form-urlencoded',
      ...hostHeader(),
    },
    body: new URLSearchParams({
      token,
      client_id: clientCredentials().clientId,
      client_secret: clientCredentials().clientSecret,
    }),
    cache: 'no-store',
  }).catch(() => {
    // Sign-out must not fail because the IdP is unreachable. The session is
    // already gone from Redis by this point, so the user is signed out here
    // whatever the authorization server says.
  })
}

function hostHeader(): Record<string, string> {
  const host = process.env.KINDLAST_OIDC_HOST_HEADER
  return host ? { Host: host } : {}
}

function redirectUri(): string {
  return requireEnv('KINDLAST_WEB_REDIRECT_URI')
}

export { safeReturnTo } from './return-to'
