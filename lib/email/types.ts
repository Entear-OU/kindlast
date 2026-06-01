/**
 * Transactional email provider seam (ENT-73).
 *
 * A narrow, processor-agnostic interface so Resend can be swapped for another
 * transactional-mail provider without touching the Comms agent. Mirrors the
 * billing/websearch provider pattern (lib/billing, lib/websearch): the factory
 * (`./provider`) is the only place that reads env and picks an implementation;
 * callers pass the resulting `EmailProvider` around as a value.
 */

export type EmailProviderName = 'resend' | 'console'

/** A single transactional message. `text` is an optional plain-text alternative. */
export interface EmailMessage {
  to: string
  subject: string
  html: string
  text?: string
}

/** The result of a send — `id` is the provider's message id (or a synthetic one). */
export interface SendResult {
  id: string
}

export interface EmailProvider {
  readonly name: EmailProviderName
  /** Deliver one message; throws on a hard failure so the caller can retry. */
  send(message: EmailMessage): Promise<SendResult>
}

export class EmailProviderError extends Error {
  constructor(
    public readonly provider: string,
    message: string,
  ) {
    super(`[email:${provider}] ${message}`)
    this.name = 'EmailProviderError'
  }
}
