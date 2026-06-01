/**
 * Email provider factory (ENT-73).
 *
 * Reads `EMAIL_PROVIDER` (default `console`) and returns the matching
 * implementation — the single place that touches email env vars. Mirrors
 * `getBillingProvider`. Callers can pass an explicit provider name or a ready
 * `override` instance; the latter keeps the dispatcher trivially mockable in
 * tests without env.
 *
 * The default is `console` (not `resend`) so local dev and CI never crash on a
 * missing key; production sets `EMAIL_PROVIDER=resend` + `RESEND_API_KEY`.
 */

import { createConsoleEmailProvider } from './console'
import { createResendProvider } from './resend'
import { EmailProviderError, type EmailProvider, type EmailProviderName } from './types'

const KNOWN_PROVIDERS: ReadonlyArray<EmailProviderName> = ['resend', 'console']

const DEFAULT_FROM = 'Kindlast <noreply@kindlast.com>'

export interface GetEmailProviderOptions {
  provider?: EmailProviderName
  /** Pre-built provider, used in tests / DI — bypasses env entirely. */
  override?: EmailProvider
}

function resolveProviderName(explicit?: EmailProviderName): EmailProviderName {
  const value = explicit ?? process.env.EMAIL_PROVIDER ?? 'console'
  if (!KNOWN_PROVIDERS.includes(value as EmailProviderName)) {
    throw new Error(
      `EMAIL_PROVIDER=${value} is not a known provider (one of: ${KNOWN_PROVIDERS.join(', ')})`,
    )
  }
  return value as EmailProviderName
}

export function getEmailProvider(options?: GetEmailProviderOptions): EmailProvider {
  if (options?.override) return options.override

  const name = resolveProviderName(options?.provider)
  switch (name) {
    case 'resend': {
      const apiKey = process.env.RESEND_API_KEY ?? ''
      if (!apiKey) {
        throw new EmailProviderError('resend', 'RESEND_API_KEY is required')
      }
      const from = process.env.EMAIL_FROM ?? DEFAULT_FROM
      return createResendProvider({ apiKey, from })
    }
    case 'console':
      return createConsoleEmailProvider()
  }
}
