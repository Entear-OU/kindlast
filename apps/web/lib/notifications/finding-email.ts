/**
 * Finding notification email template (ENT-73).
 *
 * Renders the PRD §8 four-line structure — What / Why it matters / What to do /
 * Approve · Reject · Remind me later — for a single finding. Pure: takes the
 * finding plus the base URL + token secret and returns subject/html/text, with
 * no IO, so it's fully unit-testable. The subject names the severity
 * ("[Critical] …") per the AC.
 */

import { DEFAULT_SNOOZE_DAYS, severityChip, type Finding } from '@/lib/feed/findings'

import { buildActionUrl } from './action-token'

/** The subset of a finding the email needs. */
export type FindingEmailInput = Pick<
  Finding,
  | 'id'
  | 'detected'
  | 'severity'
  | 'proposed_action'
  | 'regulatory_obligation'
  | 'citation_url'
  | 'effort_estimate'
>

export interface RenderFindingEmailOptions {
  baseUrl: string
  tokenSecret: string
  /** Epoch seconds "now" — controls the CTA token expiry (testable). */
  nowSeconds: number
  /**
   * The recipient's plan. Free recipients get a single upsell footer for the
   * Pro-only weekly briefing (ENT-74); Pro recipients see nothing extra.
   * Defaults to 'pro' (no upsell) when omitted.
   */
  plan?: 'free' | 'pro'
}

/** The Free-tier upsell footer (ENT-74): one line, only for Free recipients. */
const BRIEFING_UPSELL_TEXT =
  'Upgrade to Pro for a weekly Monday compliance briefing with your whole posture in one email.'

export interface RenderedEmail {
  subject: string
  html: string
  text: string
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

const EFFORT_LABEL: Record<Finding['effort_estimate'], string> = {
  minutes: 'a few minutes',
  hours: 'a couple of hours',
  days: 'a day or two',
}

export function renderFindingEmail(
  finding: FindingEmailInput,
  { baseUrl, tokenSecret, nowSeconds, plan = 'pro' }: RenderFindingEmailOptions,
): RenderedEmail {
  const showUpsell = plan === 'free'
  const severity = severityChip(finding.severity).label
  const subject = `[${severity}] ${finding.detected}`

  const why = finding.regulatory_obligation ?? 'A compliance obligation may apply.'
  const whatToDo = `${finding.proposed_action} (about ${EFFORT_LABEL[finding.effort_estimate]}).`

  const approveUrl = buildActionUrl(
    baseUrl,
    { findingId: finding.id, action: 'approve', nowSeconds },
    tokenSecret,
  )
  const rejectUrl = buildActionUrl(
    baseUrl,
    { findingId: finding.id, action: 'reject', nowSeconds },
    tokenSecret,
  )
  const snoozeUrl = buildActionUrl(
    baseUrl,
    { findingId: finding.id, action: 'snooze', days: DEFAULT_SNOOZE_DAYS, nowSeconds },
    tokenSecret,
  )

  const citationLine = finding.citation_url
    ? `\nReference: ${finding.citation_url}`
    : ''

  const text = [
    `[${severity}] ${finding.detected}`,
    '',
    `What: ${finding.detected}`,
    `Why it matters: ${why}${citationLine}`,
    `What to do: ${whatToDo}`,
    '',
    `Approve:        ${approveUrl}`,
    `Reject:         ${rejectUrl}`,
    `Remind me later: ${snoozeUrl}`,
    ...(showUpsell ? ['', BRIEFING_UPSELL_TEXT] : []),
  ].join('\n')

  const citationHtml = finding.citation_url
    ? ` <a href="${escapeHtml(finding.citation_url)}" style="color:#6366f1;">Read the obligation</a>`
    : ''

  const button = (href: string, label: string, bg: string, color: string) =>
    `<a href="${escapeHtml(href)}" style="display:inline-block;padding:10px 18px;margin:0 6px 8px 0;border-radius:8px;background:${bg};color:${color};font-weight:600;text-decoration:none;font-size:14px;">${label}</a>`

  const html = `<!doctype html>
<html>
  <body style="margin:0;background:#0b0b0f;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#e5e7eb;">
    <div style="max-width:560px;margin:0 auto;padding:32px 24px;">
      <p style="margin:0 0 4px;font-size:12px;letter-spacing:.08em;text-transform:uppercase;color:#a1a1aa;">${escapeHtml(severity)} finding</p>
      <h1 style="margin:0 0 20px;font-size:20px;line-height:1.3;color:#fafafa;">${escapeHtml(finding.detected)}</h1>

      <p style="margin:0 0 4px;font-size:12px;text-transform:uppercase;letter-spacing:.06em;color:#71717a;">What</p>
      <p style="margin:0 0 16px;font-size:15px;line-height:1.5;">${escapeHtml(finding.detected)}</p>

      <p style="margin:0 0 4px;font-size:12px;text-transform:uppercase;letter-spacing:.06em;color:#71717a;">Why it matters</p>
      <p style="margin:0 0 16px;font-size:15px;line-height:1.5;">${escapeHtml(why)}${citationHtml}</p>

      <p style="margin:0 0 4px;font-size:12px;text-transform:uppercase;letter-spacing:.06em;color:#71717a;">What to do</p>
      <p style="margin:0 0 24px;font-size:15px;line-height:1.5;">${escapeHtml(whatToDo)}</p>

      <div>
        ${button(approveUrl, 'Approve', '#22c55e', '#06210f')}
        ${button(rejectUrl, 'Reject', '#27272a', '#e5e7eb')}
        ${button(snoozeUrl, 'Remind me later', '#27272a', '#e5e7eb')}
      </div>

      ${showUpsell ? `<p style="margin:24px 0 0;font-size:13px;line-height:1.5;color:#a1a1aa;border-top:1px solid #27272a;padding-top:16px;">${escapeHtml(BRIEFING_UPSELL_TEXT)} <a href="${escapeHtml(baseUrl)}" style="color:#6366f1;">Upgrade</a>.</p>` : ''}
      <p style="margin:24px 0 0;font-size:12px;color:#52525b;">Kindlast, your AI compliance co-pilot. These one-tap links are private to you.</p>
    </div>
  </body>
</html>`

  return { subject, html, text }
}
