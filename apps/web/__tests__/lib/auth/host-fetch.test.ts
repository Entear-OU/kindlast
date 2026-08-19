import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { createServer, type Server } from 'node:http'
import { fetchWithHost } from '@/lib/auth/host-fetch'

/**
 * Sending a Host header the authorization server routes by (ENT-241).
 *
 * THE BUG THIS EXISTS FOR, BECAUSE IT IS NOT GUESSABLE
 *
 * `Host` is a forbidden header name in the Fetch specification, so Node's
 * global `fetch` accepts it, drops it silently, and sends the URL's own host
 * instead. No error, no warning. Zitadel routes by Host, so a request meant
 * for `localhost:8300` arrives claiming `auth:8080`, and it answers 404: the
 * discovery document appears not to exist, which sends whoever is debugging it
 * looking at URLs rather than at headers.
 *
 * It stayed invisible while the only console was a dev server on the host,
 * because there the issuer's address and the address it is fetched from are
 * the same string. A container is where they differ, which is why the
 * containerised console is what found it.
 *
 * Every assertion below is against a real server reading real headers, because
 * a mocked fetch would assert what we asked for rather than what was sent, and
 * what was sent is the entire question.
 */
describe('fetchWithHost', () => {
  let server: Server
  let origin: string

  beforeAll(async () => {
    server = createServer((request, response) => {
      const chunks: Buffer[] = []
      request.on('data', (chunk: Buffer) => chunks.push(chunk))
      request.on('end', () => {
        const body = Buffer.concat(chunks).toString('utf8')
        if (request.url === '/missing') {
          response.writeHead(404, { 'Content-Type': 'text/plain' })
          response.end('nothing here')
          return
        }
        response.writeHead(200, { 'Content-Type': 'application/json' })
        response.end(
          JSON.stringify({
            host: request.headers.host,
            method: request.method,
            contentType: request.headers['content-type'] ?? null,
            body,
          }),
        )
      })
    })

    await new Promise<void>((resolve) => {
      server.listen(0, '127.0.0.1', resolve)
    })
    const address = server.address()
    if (typeof address === 'string' || address === null) {
      throw new Error('server did not bind a port')
    }
    origin = `http://127.0.0.1:${address.port}`
  })

  afterAll(async () => {
    await new Promise<void>((resolve, reject) => {
      server.close((error) => (error ? reject(error) : resolve()))
    })
  })

  it('sends the Host the caller asked for, not the one in the URL', async () => {
    const response = await fetchWithHost(`${origin}/x`, {}, 'localhost:8300')
    const echoed = (await response.json()) as { host: string }

    // The assertion the global fetch fails. Proven by breaking it: swapping the
    // implementation for `fetch` turns this line red and leaves the rest green.
    expect(echoed.host).toBe('localhost:8300')
  })

  it('leaves the URL to speak for itself when no Host is given', async () => {
    const response = await fetchWithHost(`${origin}/x`, {})
    const echoed = (await response.json()) as { host: string }

    expect(echoed.host).toBe(origin.replace('http://', ''))
  })

  it('carries a method, a content type and a body', async () => {
    const response = await fetchWithHost(
      `${origin}/token`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({ grant_type: 'authorization_code' }),
      },
      'localhost:8300',
    )
    const echoed = (await response.json()) as {
      method: string
      contentType: string
      body: string
      host: string
    }

    expect(echoed.method).toBe('POST')
    expect(echoed.contentType).toBe('application/x-www-form-urlencoded')
    expect(echoed.body).toBe('grant_type=authorization_code')
    expect(echoed.host).toBe('localhost:8300')
  })

  it('reports a refusal as a response rather than as a throw', async () => {
    const response = await fetchWithHost(
      `${origin}/missing`,
      {},
      'localhost:8300',
    )

    // Callers branch on `ok` and on the status, the same as they would with
    // fetch. A helper that threw here would need every call site rewritten.
    expect(response.ok).toBe(false)
    expect(response.status).toBe(404)
    expect(await response.text()).toBe('nothing here')
  })

  it('rejects when the server cannot be reached at all', async () => {
    // Port 1 on loopback: nothing listens, and the failure is a connection
    // refusal rather than an HTTP status.
    await expect(fetchWithHost('http://127.0.0.1:1/x', {})).rejects.toThrow()
  })
})
