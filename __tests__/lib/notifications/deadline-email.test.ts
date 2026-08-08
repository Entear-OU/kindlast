import { describe, expect, it } from 'vitest'

import { verifyActionToken } from '@/lib/notifications/action-token'
import type { DeadlineEmailInput } from '@/lib/notifications/deadline-email'
import { renderDeadlineEmail } from '@/lib/notifications/deadline-email'

/**
 * ENT-75 — deadline alert email template.
 */

const SECRET = 'deadline-secret'
const NOW = 1_700_000_000
const BASE = 'https://app.kindlast.com'

const FINDING: DeadlineEmailInput = {
  id: '55555555-5555-5555-5555-555555555555',
  detected: 'GDPR Art. 30 ROPA effective date approaching',
  regulatory_obligation: 'GDPR Art. 30: Records of processing',
  citation_url: 'https://gdpr-info.eu/art-30-gdpr/',
  proposed_action: 'Finalise and file the record of processing activities',
}

describe('renderDeadlineEmail (ENT-75)', () => {
  const email = renderDeadlineEmail(FINDING, {
    baseUrl: BASE,
    tokenSecret: SECRET,
    nowSeconds: NOW,
    daysRemaining: 7,
    dueDate: '2026-06-09',
  })

  it('names the obligation and days remaining in the subject', () => {
    expect(email.subject).toBe('[Deadline] GDPR Art. 30: Records of processing (7 days left)')
  })

  it('singularises one day', () => {
    const one = renderDeadlineEmail(FINDING, { baseUrl: BASE, tokenSecret: SECRET, nowSeconds: NOW, daysRemaining: 1, dueDate: '2026-06-03' })
    expect(one.subject).toContain('1 day left')
  })

  it('includes obligation reference, due date, and proposed action', () => {
    expect(email.html).toContain('GDPR Art. 30')
    expect(email.html).toContain('gdpr-info.eu')
    expect(email.html).toContain('2026-06-09')
    expect(email.html).toContain('Finalise and file the record')
    expect(email.text).toContain('Due date: 2026-06-09')
  })

  it('carries a verifiable Approve token', () => {
    const url = [...email.text.matchAll(/https:\/\/\S+/g)].map((m) => m[0]).find((u) => u.includes('/findings/act'))
    expect(url).toBeDefined()
    const token = new URL(url!).searchParams.get('token')!
    const result = verifyActionToken(token, SECRET, NOW)
    expect(result.valid).toBe(true)
    if (result.valid) {
      expect(result.payload.action).toBe('approve')
      expect(result.payload.findingId).toBe(FINDING.id)
    }
  })

  it('formats a timestamp due date to a date and tolerates a null', () => {
    const ts = renderDeadlineEmail(FINDING, { baseUrl: BASE, tokenSecret: SECRET, nowSeconds: NOW, daysRemaining: 3, dueDate: '2026-06-05T23:59:00Z' })
    expect(ts.text).toContain('Due date: 2026-06-05')
    const none = renderDeadlineEmail(FINDING, { baseUrl: BASE, tokenSecret: SECRET, nowSeconds: NOW, daysRemaining: 3, dueDate: null })
    expect(none.text).toContain('Due date: soon')
  })

  it('escapes HTML in the obligation', () => {
    const xss = renderDeadlineEmail(
      { ...FINDING, regulatory_obligation: '<script>x</script>' },
      { baseUrl: BASE, tokenSecret: SECRET, nowSeconds: NOW, daysRemaining: 5, dueDate: '2026-06-07' },
    )
    expect(xss.html).not.toContain('<script>x</script>')
    expect(xss.html).toContain('&lt;script&gt;')
  })
})
