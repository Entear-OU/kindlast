import type { Metadata } from 'next'
import { SignInCard } from '@/components/auth/sign-in-card'

/**
 * The OIDC sign-in surface (ENT-197).
 *
 * A new route rather than a replacement for `/login`, deliberately. `/login`
 * is the Supabase form serving production today, and swapping it over is a
 * cut-over decision rather than a styling one; see the PR description. This
 * page is complete on its own in the meantime.
 */
export const metadata: Metadata = {
  title: 'Sign in',
  description: 'Sign in to your Kindlast compliance workspace.',
}

export default async function SignInPage({
  searchParams,
}: {
  searchParams: Promise<{ error?: string }>
}) {
  const { error } = await searchParams

  // Printed rather than described, so a self-hoster sees their own provider
  // and not a claim about ours. Derived from the configured issuer, never
  // hard-coded (§18.2).
  const issuerHost = hostOf(process.env.KINDLAST_OIDC_ISSUER)

  return (
    <SignInCard
      issuerHost={issuerHost}
      googleEnabled={process.env.KINDLAST_OIDC_GOOGLE_IDP === 'true'}
      error={error ?? null}
    />
  )
}

function hostOf(issuer: string | undefined): string {
  if (!issuer) return 'your identity provider'
  try {
    return new URL(issuer).host
  } catch {
    return 'your identity provider'
  }
}
