/**
 * Build the upgrade-page href (ENT-85). `returnTo` is the path the founder is on
 * when they hit a paywall; it rides through to /billing and becomes the checkout
 * success URL so they land back where they were trying to act. Encoded so a path
 * with query/segments survives the round-trip.
 */
export function upgradeHref(returnTo?: string): string {
  return returnTo ? `/billing?returnTo=${encodeURIComponent(returnTo)}` : '/billing'
}
