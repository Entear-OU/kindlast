/**
 * The authorization server's configuration, discovered rather than assumed.
 *
 * Every endpoint below comes from `/.well-known/openid-configuration`. Nothing
 * here hard-codes a Zitadel path, which is what lets a self-hoster point at
 * their own Keycloak, Authentik, Dex or Entra without touching this code
 * (core-api-surface §18.2). It is also an acceptance criterion on ENT-197
 * rather than a preference.
 *
 * The same discipline core-api follows, and for the same reason: the two have
 * to agree about who the issuer is, or a token web obtains is one core-api
 * refuses.
 */

import { fetchWithHost } from './host-fetch'

export interface Provider {
  issuer: string
  authorizationEndpoint: string
  tokenEndpoint: string
  revocationEndpoint: string | null
  endSessionEndpoint: string | null
  userinfoEndpoint: string | null
  jwksUri: string
}

interface DiscoveryDocument {
  issuer?: string
  authorization_endpoint?: string
  token_endpoint?: string
  revocation_endpoint?: string
  end_session_endpoint?: string
  userinfo_endpoint?: string
  jwks_uri?: string
}

export const DISCOVERY_PATH = '/.well-known/openid-configuration'

/**
 * Cached for the life of the process.
 *
 * The document changes about as often as the IdP is redeployed, and fetching
 * it per sign-in would put the authorization server in the hot path of a page
 * render, which is the thing §1.4 spends a paragraph avoiding on the core-api
 * side. A process restart re-reads it, which is a fine granularity for
 * configuration that changes at deploy time.
 */
let cached: Promise<Provider> | null = null

export function resetProviderCache(): void {
  cached = null
}

export function discoverProvider(): Promise<Provider> {
  cached ??= fetchProvider()
  return cached
}

async function fetchProvider(): Promise<Provider> {
  const issuer = requireEnv('KINDLAST_OIDC_ISSUER')

  // Where to fetch from, when that is not the issuer's own address. The
  // compose network has no `localhost:8300`, and Zitadel routes by Host, so
  // web needs the same three facts core-api does: fetch here, send this Host,
  // expect that issuer. See docs/core-api-configuration.md.
  const discoveryUrl =
    process.env.KINDLAST_OIDC_DISCOVERY_URL ??
    `${trimSlash(issuer)}${DISCOVERY_PATH}`
  const hostHeader = process.env.KINDLAST_OIDC_HOST_HEADER

  // fetchWithHost rather than fetch, and the difference is not cosmetic: the
  // Fetch specification forbids setting `Host`, so the global fetch drops it
  // without saying so and the request arrives claiming the address it was sent
  // to. Zitadel then answers 404, which reads as a missing document rather
  // than as a dropped header. See lib/auth/host-fetch.ts.
  const response = await fetchWithHost(discoveryUrl, {}, hostHeader)
  if (!response.ok) {
    cached = null
    throw new Error(
      `OIDC discovery at ${discoveryUrl} returned ${response.status}`,
    )
  }

  const document = (await response.json()) as DiscoveryDocument

  // The issuer the document claims must be the one we asked for. Without this,
  // anyone who can influence where configuration is fetched from can hand us a
  // document naming their issuer and their endpoints, and every subsequent
  // sign-in goes to them. RFC 8414 §3.3 requires the comparison.
  if (trimSlash(document.issuer ?? '') !== trimSlash(issuer)) {
    cached = null
    throw new Error(
      `OIDC discovery at ${discoveryUrl} declares issuer ${document.issuer}, expected ${issuer}`,
    )
  }

  const authorizationEndpoint = required(
    document.authorization_endpoint,
    'authorization_endpoint',
  )
  const tokenEndpoint = required(document.token_endpoint, 'token_endpoint')
  const jwksUri = required(document.jwks_uri, 'jwks_uri')

  return {
    issuer: trimSlash(issuer),
    // Endpoints are rebased onto the address they were fetched from, so the
    // only host ever contacted is the one an operator configured rather than
    // whichever one a document names. A no-op when the issuer is reachable at
    // the address it advertises, which is the ordinary case.
    authorizationEndpoint: rebase(authorizationEndpoint, discoveryUrl),
    tokenEndpoint: rebase(tokenEndpoint, discoveryUrl),
    revocationEndpoint: optionalRebase(
      document.revocation_endpoint,
      discoveryUrl,
    ),
    endSessionEndpoint: optionalRebase(
      document.end_session_endpoint,
      discoveryUrl,
    ),
    userinfoEndpoint: optionalRebase(document.userinfo_endpoint, discoveryUrl),
    jwksUri: rebase(jwksUri, discoveryUrl),
  }
}

/**
 * The authorization endpoint is the one exception to rebasing, because it is
 * the only endpoint a *browser* visits rather than this server.
 *
 * Rebasing it onto the internal address would send the user to a hostname that
 * does not resolve from their machine. So the browser-facing URL keeps the
 * issuer's own origin, which is exactly what the issuer advertises it for.
 */
export function browserAuthorizationEndpoint(provider: Provider): string {
  return rebase(provider.authorizationEndpoint, provider.issuer)
}

export function browserEndSessionEndpoint(provider: Provider): string | null {
  return provider.endSessionEndpoint
    ? rebase(provider.endSessionEndpoint, provider.issuer)
    : null
}

function rebase(endpoint: string, onto: string): string {
  const target = new URL(endpoint)
  const source = new URL(onto)

  if (target.protocol === source.protocol && target.host === source.host) {
    return endpoint
  }
  target.protocol = source.protocol
  target.host = source.host
  return target.toString()
}

function optionalRebase(
  endpoint: string | undefined,
  onto: string,
): string | null {
  return endpoint ? rebase(endpoint, onto) : null
}

function required(value: string | undefined, name: string): string {
  if (!value) {
    throw new Error(`OIDC discovery document declares no ${name}`)
  }
  return value
}

function trimSlash(value: string): string {
  return value.endsWith('/') ? value.slice(0, -1) : value
}

export function requireEnv(name: string): string {
  const value = process.env[name]
  if (!value) {
    throw new Error(`${name} must be set`)
  }
  return value
}
