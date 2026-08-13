import { describe, expect, it } from 'vitest'

import type { BriefingData } from '@/lib/notifications/briefing'
import { renderBriefingEmail } from '@/lib/notifications/briefing-email'
import { verifyUnsubscribeToken } from '@/lib/notifications/unsubscribe-token'

/**
 * ENT-74 — weekly briefing email template.
 *
 * Asserts the AC: the three sections render (open findings by severity, upcoming
 * deadlines, Executor actions last 7 days), the subject reflects the totals, and
 * the footer carries a verifiable one-tap unsubscribe link.
 */

const SECRET = 'briefing-secret'
const NOW = 1_700_000_000
const BASE = 'https://app.kindlast.com'
const USER = '44444444-4444-4444-4444-444444444444'

const DATA: BriefingData = {
  findingsBySeverity: { critical: 2, high: 1, medium: 0, low: 0 },
  openTotal: 3,
  upcomingDeadlines: [
    { label: 'GDPR Art. 30 ROPA', daysRemaining: 5 },
    { label: 'DSAR response', daysRemaining: 12 },
  ],
  executorActions: [
    { actionType: 'create_ropa', targetTable: 'processing_activities', occurredAt: '2026-05-28T10:00:00Z' },
  ],
}

describe('renderBriefingEmail (ENT-74)', () => {
  const email = renderBriefingEmail(DATA, { userId: USER, baseUrl: BASE, tokenSecret: SECRET, nowSeconds: NOW })

  it('reflects the totals in the subject', () => {
    expect(email.subject).toBe('Your weekly compliance briefing: 3 open, 2 due soon')
  })

  it('renders all three sections', () => {
    // open findings by severity
    expect(email.html).toContain('Critical')
    expect(email.text).toContain('Critical: 2')
    // deadlines
    expect(email.html).toContain('GDPR Art. 30')
    expect(email.html).toContain('DSAR response')
    // executor actions (friendly label)
    expect(email.html).toContain('Created a record of processing')
  })

  it('shows empty-state copy when a section has nothing', () => {
    const empty = renderBriefingEmail(
      { findingsBySeverity: { critical: 0, high: 0, medium: 0, low: 0 }, openTotal: 0, upcomingDeadlines: [], executorActions: [] },
      { userId: USER, baseUrl: BASE, tokenSecret: SECRET, nowSeconds: NOW },
    )
    expect(empty.text).toContain('None in the next 30 days.')
    expect(empty.text).toContain('No actions taken in the last 7 days.')
  })

  it('carries a verifiable unsubscribe link in the footer', () => {
    const url = [...email.text.matchAll(/https:\/\/\S+/g)].map((m) => m[0]).find((u) => u.includes('/unsubscribe'))
    expect(url).toBeDefined()
    const token = new URL(url!).searchParams.get('token')!
    const result = verifyUnsubscribeToken(token, SECRET, NOW)
    expect(result.valid).toBe(true)
    if (result.valid) expect(result.payload.userId).toBe(USER)
  })

  it('escapes HTML in deadline labels', () => {
    const xss = renderBriefingEmail(
      { ...DATA, upcomingDeadlines: [{ label: '<script>x</script>', daysRemaining: 1 }] },
      { userId: USER, baseUrl: BASE, tokenSecret: SECRET, nowSeconds: NOW },
    )
    expect(xss.html).not.toContain('<script>x</script>')
    expect(xss.html).toContain('&lt;script&gt;')
  })
})
