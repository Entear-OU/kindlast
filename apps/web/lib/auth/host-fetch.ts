import { request as httpRequest } from 'node:http'
import { request as httpsRequest } from 'node:https'
import type { ClientRequest, IncomingMessage } from 'node:http'

/**
 * A request that can name a Host the authorization server routes by.
 *
 * WHY THIS IS NOT `fetch`
 *
 * `Host` is a forbidden header name in the Fetch specification. Node's global
 * fetch accepts one, drops it, and sends the URL's own authority instead. No
 * error and no warning, so the only symptom is the response you get from
 * whichever virtual server answered instead of the one you meant.
 *
 * That matters here because the bundled stack has a split horizon: the browser
 * reaches Zitadel at `localhost:8300`, containers reach the same process at
 * `auth:8080`, and Zitadel routes by Host, so a request from a container has
 * to arrive claiming the browser's address. core-api has always done this
 * (KINDLAST_OIDC_HOST_HEADER), and Go makes it trivial because `http.Request`
 * carries `Host` as a field rather than a header.
 *
 * Measured against the running stack rather than reasoned about: with the
 * header passed to fetch, discovery at http://auth:8080 returns 404. With the
 * same header through node:http it returns 200. The 404 is what makes this
 * expensive to debug, because it reads as a missing document rather than a
 * dropped header.
 *
 * WHY NOT ALWAYS USE THIS
 *
 * Without an override there is nothing to fix, and the platform's fetch is
 * better at everything else: connection pooling, redirects, compression,
 * abort signals. So an ordinary call stays an ordinary call, and this path is
 * taken only where a deployment has said the two addresses differ. That also
 * keeps `bun run dev` and any single-address deployment on exactly the code
 * they were on before.
 *
 * Server only. It imports node:http, so it must never be reachable from the
 * edge runtime the proxy runs in.
 */
export interface HostFetchInit {
  method?: string
  headers?: Record<string, string>
  body?: URLSearchParams | string
}

export async function fetchWithHost(
  url: string,
  init: HostFetchInit = {},
  hostHeader?: string,
): Promise<Response> {
  const target = new URL(url)
  const host = hostHeader?.trim()

  if (!host || host === target.host) {
    return fetch(url, {
      method: init.method,
      headers: init.headers,
      body: init.body,
      // Nothing here is cacheable: a discovery document is read once per
      // process and a token exchange is single use.
      cache: 'no-store',
    })
  }

  const body = init.body === undefined ? undefined : String(init.body)
  const headers: Record<string, string> = { ...init.headers, Host: host }
  if (body !== undefined) {
    // Node does not set this for us the way fetch does, and an authorization
    // server reading a request with no length has to guess.
    headers['Content-Length'] = String(Buffer.byteLength(body))
  }

  const send = target.protocol === 'https:' ? httpsRequest : httpRequest

  return new Promise<Response>((resolve, reject) => {
    const outbound: ClientRequest = send(
      {
        protocol: target.protocol,
        hostname: target.hostname,
        port: target.port,
        path: `${target.pathname}${target.search}`,
        method: init.method ?? 'GET',
        headers,
      },
      (incoming: IncomingMessage) => {
        const chunks: Buffer[] = []
        incoming.on('data', (chunk: Buffer) => chunks.push(chunk))
        incoming.on('error', reject)
        incoming.on('end', () => {
          // Handed back as a Response so callers read `ok`, `status`, `json()`
          // and `text()` exactly as they would from fetch. A helper with its
          // own result shape would mean rewriting every call site to gain
          // nothing.
          resolve(
            new Response(Buffer.concat(chunks), {
              status: incoming.statusCode ?? 502,
              headers: singleValued(incoming.headers),
            }),
          )
        })
      },
    )

    outbound.on('error', reject)
    if (body !== undefined) outbound.write(body)
    outbound.end()
  })
}

/**
 * Node hands back repeated headers as arrays, and `set-cookie` always as one.
 * The Headers constructor wants strings, and nothing this is used for reads a
 * repeated header, so they are joined rather than dropped.
 */
function singleValued(
  headers: IncomingMessage['headers'],
): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [name, value] of Object.entries(headers)) {
    if (value === undefined) continue
    out[name] = Array.isArray(value) ? value.join(', ') : value
  }
  return out
}
