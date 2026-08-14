/**
 * Resend implementation of the EmailProvider seam (ENT-73).
 *
 * A thin `fetch` wrapper over the Resend REST API — no SDK dependency. The
 * factory (`./provider`) constructs this with the API key and the verified
 * `from` address; a non-2xx response throws an EmailProviderError so the
 * dispatcher leaves the outbox row pending for a later retry.
 */

import {
  EmailProviderError,
  type EmailMessage,
  type EmailProvider,
  type SendResult,
} from './types'

const RESEND_ENDPOINT = 'https://api.resend.com/emails'

export interface ResendProviderConfig {
  apiKey: string
  /** Verified sender, e.g. `Kindlast <noreply@kindlast.com>`. */
  from: string
}

export function createResendProvider({
  apiKey,
  from,
}: ResendProviderConfig): EmailProvider {
  return {
    name: 'resend',
    async send(message: EmailMessage): Promise<SendResult> {
      const response = await fetch(RESEND_ENDPOINT, {
        method: 'POST',
        headers: {
          authorization: `Bearer ${apiKey}`,
          'content-type': 'application/json',
        },
        body: JSON.stringify({
          from,
          to: message.to,
          subject: message.subject,
          html: message.html,
          ...(message.text ? { text: message.text } : {}),
        }),
      })

      if (!response.ok) {
        const detail = await response.text().catch(() => '')
        throw new EmailProviderError(
          'resend',
          `send failed (${response.status})${detail ? `: ${detail}` : ''}`,
        )
      }

      const body = (await response.json().catch(() => ({}))) as { id?: string }
      return { id: body.id ?? 'unknown' }
    },
  }
}
