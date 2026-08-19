import { readFileSync } from 'node:fs'

/**
 * The console's OAuth client credentials, from the environment or from the
 * file the seed writes (ENT-241).
 *
 * TWO SHAPES, BECAUSE THE TWO DEPLOYMENTS LOOK DIFFERENT
 *
 * A self-hoster registers a confidential client with whatever authorization
 * server they run and sets KINDLAST_OIDC_CLIENT_ID and
 * KINDLAST_OIDC_CLIENT_SECRET, the same as any other configuration.
 *
 * The bundled compose stack cannot do that, because Zitadel generates the
 * secret when the seed job creates the application, which is long after
 * compose was written. The seed writes it to the shared volume as
 * `web-client.json`, and KINDLAST_OIDC_CLIENT_FILE points here. That is what
 * lets `docker compose up -d` produce a console that can complete a sign-in
 * with no manual step in between, which is the whole point of ENT-241.
 *
 * This is core-api's `internalClient` in TypeScript: same precedence, same
 * JSON field names, same "the environment wins" rule so a real deployment
 * never depends on a file. Two mechanisms for one problem in one repository
 * would mean one of them is wrong.
 *
 * The field names are Zitadel's rather than ours. The seed stores the response
 * as it was returned instead of translating it, which is one fewer place that
 * can silently stop matching.
 */
export interface ClientCredentials {
  clientId: string
  clientSecret: string
}

/**
 * Read once per process.
 *
 * The alternative is reading a file on every sign-in, refresh and sign-out,
 * which puts a volume in the hot path of the auth flow to re-read a value that
 * changes when the deployment is rebuilt. A restart re-reads it, which is the
 * granularity configuration changes at.
 */
let cached: ClientCredentials | null = null

export function resetClientCredentialsCache(): void {
  cached = null
}

export function clientCredentials(): ClientCredentials {
  cached ??= resolve()
  return cached
}

function resolve(): ClientCredentials {
  const id = process.env.KINDLAST_OIDC_CLIENT_ID?.trim()
  const secret = process.env.KINDLAST_OIDC_CLIENT_SECRET?.trim()
  const path = process.env.KINDLAST_OIDC_CLIENT_FILE?.trim()

  if (id && secret) return { clientId: id, clientSecret: secret }

  // Half a credential is a misconfiguration rather than a fallback. Falling
  // through to the file here would silently use a different client from the
  // one the operator was configuring, and the sign-in that then fails would
  // point at the wrong half.
  if (id && !secret && !path) {
    throw new Error('KINDLAST_OIDC_CLIENT_SECRET must be set')
  }
  if (secret && !id && !path) {
    throw new Error('KINDLAST_OIDC_CLIENT_ID must be set')
  }

  if (!path) {
    throw new Error(
      'KINDLAST_OIDC_CLIENT_ID and KINDLAST_OIDC_CLIENT_SECRET must be set, or KINDLAST_OIDC_CLIENT_FILE must name a file holding them',
    )
  }

  let raw: string
  try {
    raw = readFileSync(path, 'utf8')
  } catch (cause) {
    // The path is in the message on purpose. The likeliest cause is a volume
    // that was never mounted, and the likeliest reader is somebody who did not
    // know a file was involved at all.
    throw new Error(`KINDLAST_OIDC_CLIENT_FILE ${path} could not be read`, {
      cause,
    })
  }

  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch (cause) {
    throw new Error(`KINDLAST_OIDC_CLIENT_FILE ${path} is not valid JSON`, {
      cause,
    })
  }

  const credential = parsed as Partial<ClientCredentials>
  const fileId = credential?.clientId?.trim()
  const fileSecret = credential?.clientSecret?.trim()

  if (!fileId) {
    throw new Error(`KINDLAST_OIDC_CLIENT_FILE ${path} declares no clientId`)
  }
  if (!fileSecret) {
    throw new Error(
      `KINDLAST_OIDC_CLIENT_FILE ${path} declares no clientSecret`,
    )
  }

  return { clientId: fileId, clientSecret: fileSecret }
}
