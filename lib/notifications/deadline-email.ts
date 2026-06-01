/**
 * Deadline alert email template (ENT-75).
 *
 * A deadline-framed message for an obligation/DSAR entering the 30/14/7/1-day
 * window: names the obligation and days remaining in the subject, and lays out
 * the obligation reference, due date, proposed action, and a one-tap Approve
 * link in the body. Pure (no IO) — fully unit-testable. Reuses ENT-73's signed
 * action token for the Approve CTA.
 */

import type { Finding } from '@/lib/feed/findings'

import { buildActionUrl } from './action-token'

export type DeadlineEmailInput = Pick<
  Finding,
  'id' | 'detected' | 'regulatory_obligation' | 'citation_url' | 'proposed_action'
>

export interface RenderDeadlineEmailOptions {
  baseUrl: string
  tokenSecret: string
  /** Epoch seconds "now" — controls the Approve token expiry (testable). */
  nowSeconds: number
  daysRemaining: number
  /** ISO date/timestamp of the deadline; null when unknown. */
  dueDate: string | null
}

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

/** YYYY-MM-DD from an ISO date or timestamp; passthrough if unparseable. */
function formatDueDate(dueDate: string | null): string {
  if (!dueDate) return 'soon'
  const iso = dueDate.slice(0, 10)
  return /^\d{4}-\d{2}-\d{2}$/.test(iso) ? iso : dueDate
}

export function renderDeadlineEmail(
  finding: DeadlineEmailInput,
  { baseUrl, tokenSecret, nowSeconds, daysRemaining, dueDate }: RenderDeadlineEmailOptions,
): RenderedEmail {
  const obligation = finding.regulatory_obligation ?? finding.detected
  const days = Math.max(0, daysRemaining)
  const dayLabel = `${days} day${days === 1 ? '' : 's'}`
  const due = formatDueDate(dueDate)
  const subject = `[Deadline] ${obligation} — ${dayLabel} left`

  const approveUrl = buildActionUrl(
    baseUrl,
    { findingId: finding.id, action: 'approve', nowSeconds },
    tokenSecret,
  )

  const citationLine = finding.citation_url ? `\nReference: ${finding.citation_url}` : ''
  const text = [
    `${obligation} — ${dayLabel} until the deadline`,
    '',
    `Obligation: ${obligation}${citationLine}`,
    `Due date: ${due} (${dayLabel} remaining)`,
    `What to do: ${finding.proposed_action}`,
    '',
    `Approve: ${approveUrl}`,
  ].join('\n')

  const citationHtml = finding.citation_url
    ? ` <a href="${escapeHtml(finding.citation_url)}" style="color:#6366f1;">Read the obligation</a>`
    : ''

  const html = `<!doctype html>
<html>
  <body style="margin:0;background:#0b0b0f;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#e5e7eb;">
    <div style="max-width:560px;margin:0 auto;padding:32px 24px;">
      <p style="margin:0 0 4px;font-size:12px;letter-spacing:.08em;text-transform:uppercase;color:#f59e0b;">Deadline in ${escapeHtml(dayLabel)}</p>
      <h1 style="margin:0 0 20px;font-size:20px;line-height:1.3;color:#fafafa;">${escapeHtml(obligation)}</h1>

      <p style="margin:0 0 4px;font-size:12px;text-transform:uppercase;letter-spacing:.06em;color:#71717a;">Obligation</p>
      <p style="margin:0 0 16px;font-size:15px;line-height:1.5;">${escapeHtml(obligation)}${citationHtml}</p>

      <p style="margin:0 0 4px;font-size:12px;text-transform:uppercase;letter-spacing:.06em;color:#71717a;">Due date</p>
      <p style="margin:0 0 16px;font-size:15px;line-height:1.5;">${escapeHtml(due)} — <strong>${escapeHtml(dayLabel)}</strong> remaining</p>

      <p style="margin:0 0 4px;font-size:12px;text-transform:uppercase;letter-spacing:.06em;color:#71717a;">What to do</p>
      <p style="margin:0 0 24px;font-size:15px;line-height:1.5;">${escapeHtml(finding.proposed_action)}</p>

      <div>
        <a href="${escapeHtml(approveUrl)}" style="display:inline-block;padding:10px 18px;border-radius:8px;background:#22c55e;color:#06210f;font-weight:600;text-decoration:none;font-size:14px;">Approve</a>
      </div>

      <p style="margin:24px 0 0;font-size:12px;color:#52525b;">Kindlast — your AI compliance co-pilot. This one-tap link is private to you.</p>
    </div>
  </body>
</html>`

  return { subject, html, text }
}
