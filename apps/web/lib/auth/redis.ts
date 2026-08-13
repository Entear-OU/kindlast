/**
 * The shared Redis connection for pre-auth state and sessions.
 *
 * One client per process, reused, because Next.js route handlers run per
 * request and a connection per request exhausts the server's file descriptors
 * long before it exhausts anything else.
 *
 * What lives here, and what must not (core-api-surface §15.1, §15.2): sessions
 * and short-lived pre-auth state, both of which can be rebuilt by signing in
 * again. No findings, no records, no tenant-scoped rows. Caching those would
 * move them outside the RLS boundary, where a key-construction bug becomes a
 * cross-tenant leak with no database policy left to catch it.
 */
import Redis from 'ioredis'

let client: Redis | null = null

export function redis(): Redis {
  if (client) return client

  const url = process.env.KINDLAST_REDIS_URL ?? 'redis://127.0.0.1:6379'

  client = new Redis(url, {
    // Fail a request rather than queue it forever behind a dead Redis. A
    // sign-in that hangs is worse than one that errors: the user retries, and
    // the retries pile up on the same unreachable connection.
    maxRetriesPerRequest: 2,
    lazyConnect: false,
  })

  return client
}

/** For tests, which need a client that does not outlive the suite. */
export function resetRedis(): void {
  client?.disconnect()
  client = null
}
