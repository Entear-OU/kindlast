import { describe, it, expect } from 'vitest'
import { GET, dynamic } from '@/app/healthz/route'

/**
 * The liveness endpoint the container's healthcheck probes (ENT-241).
 *
 * Two properties are worth asserting, and neither is "it returns 200" for its
 * own sake.
 *
 * It must answer without reaching Redis, core-api or the authorization server.
 * A healthcheck that depends on the stack around it reports a dependency's
 * outage as this container being unhealthy, so an orchestrator restarts a
 * process that was working and the real fault ends up one restart further
 * away.
 *
 * It must not be prerendered. A static answer proves the file was built, not
 * that the server is running, which is the entire question a healthcheck asks.
 */
describe('GET /healthz', () => {
  it('answers 200 with a body a human can recognise', async () => {
    const response = GET()

    expect(response.status).toBe(200)
    expect(await response.text()).toBe('ok')
  })

  it('is not cached, so a probe reflects the process rather than a build', () => {
    expect(dynamic).toBe('force-dynamic')
  })

  it('touches nothing: no network call of any kind', () => {
    const realFetch = globalThis.fetch
    globalThis.fetch = () => {
      throw new Error('healthz must not make a network call')
    }
    try {
      expect(GET().status).toBe(200)
    } finally {
      globalThis.fetch = realFetch
    }
  })
})
