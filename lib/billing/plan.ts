/**
 * Billing seam (ENT-63).
 *
 * The product has no subscriptions store yet — that's ENT-81. Until then this is
 * the single place every tier decision flows through: it reports Pro for everyone,
 * so the feed's Approve path (which gates on the plan) works end-to-end while the
 * upgrade-prompt branch stays wired but dormant. When billing lands, ENT-81
 * replaces the body here with a real lookup and nothing else has to change.
 */

export type Plan = 'free' | 'pro'

export async function getPlan(_userId: string): Promise<Plan> {
  return 'pro'
}
