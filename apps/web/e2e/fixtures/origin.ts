/**
 * Which console these tests are driving (ENT-241).
 *
 * There are two now. `bun run dev` serves one on port 3000, and the compose
 * stack serves a production build of the same app behind the edge. The suite
 * has to be able to drive either, because they fail differently: the dev
 * server compiles on demand and forgives things a production build does not,
 * and the containerised console is the artefact a self-hoster actually runs.
 *
 * KINDLAST_WEB_URL picks one. playwright.config.ts already read it for
 * `baseURL`, so relative navigation was portable; what was not portable were
 * the URL assertions, which spelled out `localhost:3000` and therefore passed
 * only against the dev server. A test that can only pass against one of two
 * deployments is a test that quietly stops covering the other.
 */
const RAW = process.env.KINDLAST_WEB_URL ?? 'http://localhost:3000'

/** The origin under test, without a trailing slash. */
export const WEB_ORIGIN = RAW.replace(/\/+$/, '')

/** Its host and port, for assertions about having left our own origin. */
export const WEB_HOST = new URL(WEB_ORIGIN).host

/**
 * A pattern anchored at the console's origin.
 *
 * The origin is escaped because it is a literal, and the argument is not,
 * because callers are matching path shapes: `onOrigin('/o/[a-z0-9-]+$')`.
 */
export function onOrigin(pathPattern = ''): RegExp {
  return new RegExp(`^${escapeLiteral(WEB_ORIGIN)}${pathPattern}`)
}

function escapeLiteral(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}
