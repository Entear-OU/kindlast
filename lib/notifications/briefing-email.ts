/**
 * Weekly briefing email template (ENT-74).
 *
 * Renders the Monday posture digest: open findings by severity, deadlines within
 * 30 days, and the Executor actions taken last week — plus a one-tap unsubscribe
 * link in the footer. Pure (no IO), so it's fully unit-testable. The briefing is
 * Pro-only, so there's no upsell here; the Free-tier upsell lives in the finding
 * email (ENT-73).
 */

import { severityChip, type FindingSeverity } from '@/lib/feed/findings'

import type { BriefingData } from './briefing'
import { buildUnsubscribeUrl } from './unsubscribe-token'

export interface RenderBriefingOptions {
  userId: string
  baseUrl: string
  tokenSecret: string
  /** Epoch seconds "now" — controls the unsubscribe token expiry (testable). */
  nowSeconds: number
}

export interface RenderedEmail {
  subject: string
  html: string
  text: string
}

const SEVERITY_ORDER: FindingSeverity[] = ['critical', 'high', 'medium', 'low']

const ACTION_LABEL: Record<string, string> = {
  create_ropa: 'Created a record of processing (ROPA)',
  mark_dsar_responded: 'Logged a data-subject-request response',
  create_ai_system: 'Registered an AI system',
  approve_finding: 'Approved a finding',
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function actionLabel(actionType: string): string {
  return ACTION_LABEL[actionType] ?? actionType.replace(/_/g, ' ')
}

export function renderBriefingEmail(
  data: BriefingData,
  { userId, baseUrl, tokenSecret, nowSeconds }: RenderBriefingOptions,
): RenderedEmail {
  const dueSoon = data.upcomingDeadlines.length
  const subject = `Your weekly compliance briefing: ${data.openTotal} open, ${dueSoon} due soon`

  const unsubscribeUrl = buildUnsubscribeUrl(
    baseUrl,
    { userId, scope: 'weekly_briefing', nowSeconds },
    tokenSecret,
  )

  // ── plain text ──
  const severityLines = SEVERITY_ORDER
    .filter((s) => data.findingsBySeverity[s] > 0)
    .map((s) => `  ${severityChip(s).label}: ${data.findingsBySeverity[s]}`)
  const deadlineLines = data.upcomingDeadlines.length
    ? data.upcomingDeadlines.map((d) => `  ${d.label}: ${d.daysRemaining} day(s)`)
    : ['  None in the next 30 days.']
  const actionLines = data.executorActions.length
    ? data.executorActions.map((a) => `  ${actionLabel(a.actionType)}`)
    : ['  No actions taken in the last 7 days.']

  const text = [
    'Your weekly compliance briefing',
    '',
    `Open findings (${data.openTotal}):`,
    ...(severityLines.length ? severityLines : ['  None open.']),
    '',
    'Upcoming deadlines (next 30 days):',
    ...deadlineLines,
    '',
    'What shipped (last 7 days):',
    ...actionLines,
    '',
    `Stop weekly briefings: ${unsubscribeUrl}`,
  ].join('\n')

  // ── html ──
  const severityHtml = SEVERITY_ORDER
    .filter((s) => data.findingsBySeverity[s] > 0)
    .map(
      (s) =>
        `<li style="margin:0 0 4px;">${escapeHtml(severityChip(s).label)}: <strong>${data.findingsBySeverity[s]}</strong></li>`,
    )
    .join('')
  const deadlinesHtml = data.upcomingDeadlines.length
    ? data.upcomingDeadlines
        .map(
          (d) =>
            `<li style="margin:0 0 4px;">${escapeHtml(d.label)}: <strong>${d.daysRemaining}</strong> day${d.daysRemaining === 1 ? '' : 's'}</li>`,
        )
        .join('')
    : '<li style="margin:0 0 4px;color:#71717a;">None in the next 30 days.</li>'
  const actionsHtml = data.executorActions.length
    ? data.executorActions
        .map((a) => `<li style="margin:0 0 4px;">${escapeHtml(actionLabel(a.actionType))}</li>`)
        .join('')
    : '<li style="margin:0 0 4px;color:#71717a;">No actions taken in the last 7 days.</li>'

  const section = (title: string, body: string) =>
    `<p style="margin:24px 0 6px;font-size:12px;text-transform:uppercase;letter-spacing:.06em;color:#71717a;">${title}</p>
     <ul style="margin:0;padding-left:18px;font-size:15px;line-height:1.5;">${body}</ul>`

  const html = `<!doctype html>
<html>
  <body style="margin:0;background:#0b0b0f;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#e5e7eb;">
    <div style="max-width:560px;margin:0 auto;padding:32px 24px;">
      <p style="margin:0 0 4px;font-size:12px;letter-spacing:.08em;text-transform:uppercase;color:#a1a1aa;">Monday briefing</p>
      <h1 style="margin:0 0 8px;font-size:20px;line-height:1.3;color:#fafafa;">Your week in compliance</h1>
      <p style="margin:0;font-size:14px;color:#a1a1aa;">${data.openTotal} open finding${data.openTotal === 1 ? '' : 's'}, ${dueSoon} due in the next 30 days.</p>

      ${section(`Open findings (${data.openTotal})`, severityHtml || '<li style="margin:0 0 4px;color:#71717a;">None open.</li>')}
      ${section('Upcoming deadlines', deadlinesHtml)}
      ${section('What shipped last week', actionsHtml)}

      <p style="margin:28px 0 0;font-size:12px;color:#52525b;">
        Kindlast, your AI compliance co-pilot.
        <a href="${escapeHtml(unsubscribeUrl)}" style="color:#52525b;text-decoration:underline;">Stop weekly briefings</a>.
      </p>
    </div>
  </body>
</html>`

  return { subject, html, text }
}
