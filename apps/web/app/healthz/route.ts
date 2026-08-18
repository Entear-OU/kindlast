/**
 * Liveness, and nothing more (ENT-241).
 *
 * The compose stack runs this app as a container behind the edge, and the edge
 * must not route to a `web` that is not serving yet. That needs a probe, and
 * the shape of the probe is a decision rather than a formality.
 *
 * WHAT THIS DELIBERATELY DOES NOT CHECK
 *
 * It does not open Redis, call core-api, or fetch the OIDC discovery document.
 * A healthcheck that did would report "Redis is down" as "web is unhealthy",
 * and an orchestrator would then restart a process that was working perfectly
 * while the actual fault stayed where it was. Readiness for a given dependency
 * belongs on the page that needs it, where a person can be told which part is
 * unavailable.
 *
 * WHAT PROVES THE APP ACTUALLY WORKS, SINCE IT IS NOT THIS
 *
 * The build does. `next build` compiles every route and prerenders the static
 * ones, so a page that only fails under a production build fails the image
 * build rather than reaching a container. Beyond that it is `bun run test:e2e`
 * pointed at KINDLAST_WEB_URL, which drives the built artefact through a real
 * browser. This endpoint answers one question: is the server up.
 *
 * The edge answers `/healthz` itself, for the stack as a whole, so this one is
 * reachable on the compose network rather than through the front door. They
 * are different questions and it is right that they have different answers.
 */

/**
 * Never prerendered.
 *
 * A statically generated healthcheck is a file, and a file being served proves
 * the build happened, not that the process is alive. Exported so a test can
 * assert it: getting this wrong produces a check that cannot fail, which is
 * worse than having no check at all.
 */
export const dynamic = 'force-dynamic'

export function GET(): Response {
  return new Response('ok', {
    status: 200,
    headers: {
      'Content-Type': 'text/plain; charset=utf-8',
      'Cache-Control': 'no-store',
    },
  })
}
