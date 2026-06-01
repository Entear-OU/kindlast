import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { createCapturingEmailProvider } from '@/lib/email/console'
import { getEmailProvider } from '@/lib/email/provider'
import { EmailProviderError } from '@/lib/email/types'

/**
 * ENT-73 — email provider factory seam.
 *
 * Asserts the env-driven selection: default `console` (so dev/CI never crash on
 * a missing key), explicit `resend` requires a key, and an `override` bypasses
 * env entirely for tests/DI.
 */

describe('getEmailProvider (ENT-73)', () => {
  const ORIGINAL = { ...process.env }

  beforeEach(() => {
    delete process.env.EMAIL_PROVIDER
    delete process.env.RESEND_API_KEY
    delete process.env.EMAIL_FROM
  })

  afterEach(() => {
    process.env = { ...ORIGINAL }
    vi.restoreAllMocks()
  })

  it('defaults to the console provider when EMAIL_PROVIDER is unset', () => {
    expect(getEmailProvider().name).toBe('console')
  })

  it('returns the resend provider when selected and a key is present', () => {
    process.env.EMAIL_PROVIDER = 'resend'
    process.env.RESEND_API_KEY = 're_test_key'
    expect(getEmailProvider().name).toBe('resend')
  })

  it('throws a clear error when resend is selected without a key', () => {
    process.env.EMAIL_PROVIDER = 'resend'
    expect(() => getEmailProvider()).toThrow(EmailProviderError)
  })

  it('rejects an unknown provider name', () => {
    process.env.EMAIL_PROVIDER = 'mailgun'
    expect(() => getEmailProvider()).toThrow(/not a known provider/)
  })

  it('returns the override untouched, ignoring env', () => {
    process.env.EMAIL_PROVIDER = 'resend' // would otherwise throw (no key)
    const override = createCapturingEmailProvider()
    expect(getEmailProvider({ override })).toBe(override)
  })
})
