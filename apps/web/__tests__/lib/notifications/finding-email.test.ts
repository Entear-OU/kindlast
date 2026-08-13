import { describe, expect, it } from 'vitest'

import type { FindingEmailInput } from '@/lib/notifications/finding-email'
import { renderFindingEmail } from '@/lib/notifications/finding-email'
import { verifyActionToken } from '@/lib/notifications/action-token'

/**
 * ENT-73 — finding notification email template.
 *
 * Asserts the AC: subject names the severity, the body carries the four-line
 * structure (What / Why it matters / What to do / CTAs), and each CTA deep-links
 * with a verifiable one-tap token for the right action.
 */

const SECRET = 'render-secret'
const NOW = 1_700_000_000
const BASE = 'https://app.kindlast.com'

const FINDING: FindingEmailInput = {
  id: '22222222-2222-2222-2222-222222222222',
  detected: 'DSAR response due in 8 days',
  severity: 'critical',
  proposed_action: 'Draft and send the access-request response',
  regulatory_obligation: 'GDPR Art. 12(3) — respond within one month',
  citation_url: 'https://gdpr-info.eu/art-12-gdpr/',
  effort_estimate: 'hours',
}

function tokenFor(url: string): string | null {
  return new URL(url).searchParams.get('token')
}

describe('renderFindingEmail (ENT-73)', () => {
  const email = renderFindingEmail(FINDING, { baseUrl: BASE, tokenSecret: SECRET, nowSeconds: NOW })

  it('names the severity in the subject', () => {
    expect(email.subject).toBe('[Critical] DSAR response due in 8 days')
  })

  it('includes all four lines of the PRD §8 structure', () => {
    for (const label of ['What', 'Why it matters', 'What to do']) {
      expect(email.html).toContain(label)
      expect(email.text).toContain(label)
    }
    expect(email.text).toContain('Approve:')
    expect(email.text).toContain('Reject:')
    expect(email.text).toContain('Remind me later')
  })

  it('carries the obligation and citation', () => {
    expect(email.html).toContain('GDPR Art. 12(3)')
    expect(email.html).toContain('gdpr-info.eu')
  })

  it('embeds a valid, action-specific token in each CTA', () => {
    const urls = [...email.text.matchAll(/https:\/\/\S+/g)]
      .map((m) => m[0])
      .filter((u) => tokenFor(u) !== null)
    const byAction = (action: string) =>
      urls.find((u) => {
        const result = verifyActionToken(tokenFor(u)!, SECRET, NOW)
        return result.valid && result.payload.action === action
      })

    for (const action of ['approve', 'reject', 'snooze']) {
      const url = byAction(action)
      expect(url, `missing ${action} CTA`).toBeDefined()
      const result = verifyActionToken(tokenFor(url!)!, SECRET, NOW)
      expect(result.valid).toBe(true)
      if (result.valid) expect(result.payload.findingId).toBe(FINDING.id)
    }
  })

  it('adds the weekly-briefing upsell footer for a Free recipient', () => {
    const free = renderFindingEmail(FINDING, { baseUrl: BASE, tokenSecret: SECRET, nowSeconds: NOW, plan: 'free' })
    expect(free.text).toContain('Upgrade to Pro for a weekly Monday compliance briefing')
    expect(free.html).toContain('weekly Monday compliance briefing')
  })

  it('omits the upsell for a Pro recipient (and by default)', () => {
    const pro = renderFindingEmail(FINDING, { baseUrl: BASE, tokenSecret: SECRET, nowSeconds: NOW, plan: 'pro' })
    expect(pro.text).not.toContain('Upgrade to Pro for a weekly')
    // default (omitted plan) is pro → no upsell
    expect(email.text).not.toContain('Upgrade to Pro for a weekly')
  })

  it('escapes HTML in finding text to avoid breaking markup', () => {
    const xss = renderFindingEmail(
      { ...FINDING, detected: 'Tag <script>alert(1)</script> & "quotes"' },
      { baseUrl: BASE, tokenSecret: SECRET, nowSeconds: NOW },
    )
    expect(xss.html).not.toContain('<script>')
    expect(xss.html).toContain('&lt;script&gt;')
  })
})
