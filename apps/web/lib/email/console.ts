/**
 * Console implementation of the EmailProvider seam (ENT-73).
 *
 * The default when `EMAIL_PROVIDER` is unset — logs the message instead of
 * sending so local dev and CI never need a real API key or crash on a missing
 * one. Tests use the in-memory `createCapturingEmailProvider` (below) to assert
 * on what would have been sent.
 */

import type { EmailMessage, EmailProvider, SendResult } from './types'

let counter = 0

export function createConsoleEmailProvider(): EmailProvider {
  return {
    name: 'console',
    async send(message: EmailMessage): Promise<SendResult> {
      counter += 1
      const id = `console-${counter}`
      console.info(`[email:console] → ${message.to} | ${message.subject} (${id})`)
      return { id }
    },
  }
}

/**
 * An in-memory provider that records every message — for tests and the
 * integration dispatcher. Exposes `.sent` so assertions can inspect the
 * rendered subject / html / recipient without a network call.
 */
export interface CapturingEmailProvider extends EmailProvider {
  readonly sent: EmailMessage[]
}

export function createCapturingEmailProvider(): CapturingEmailProvider {
  const sent: EmailMessage[] = []
  return {
    name: 'console',
    sent,
    async send(message: EmailMessage): Promise<SendResult> {
      sent.push(message)
      return { id: `captured-${sent.length}` }
    },
  }
}
