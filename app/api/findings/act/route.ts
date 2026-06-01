import { performFindingAction, type ActionResult } from '@/lib/notifications/act'
import { createServiceRoleClient } from '@/lib/supabase/service-role'

/**
 * One-tap finding action endpoint (ENT-73).
 *
 * The Approve / Reject / Remind-me-later buttons in a finding email link here
 * with a signed token. It's a GET (clicked from a mail client into a browser),
 * verifies the token, applies the action via the service role, and returns a
 * small branded HTML confirmation page. Invalid/expired tokens 400; an unknown
 * finding 404s; a Free owner hitting Approve gets the upgrade page.
 */

export const dynamic = 'force-dynamic'

const ACTION_VERB: Record<string, string> = {
  approve: 'approved',
  reject: 'rejected',
  snooze: 'snoozed',
}

interface PageCopy {
  status: number
  title: string
  message: string
}

function pageCopy(result: ActionResult): PageCopy {
  const verb = result.action ? ACTION_VERB[result.action] : 'updated'
  switch (result.kind) {
    case 'ok':
      return { status: 200, title: `Finding ${verb}`, message: `Done — this finding is now ${verb}. You can close this tab.` }
    case 'noop':
      return { status: 200, title: 'Already actioned', message: 'This finding was already handled. Nothing else to do.' }
    case 'upgrade':
      return {
        status: 402,
        title: 'Upgrade to act',
        message: 'Approving a finding fires the Executor — a Pro feature. Open Kindlast to upgrade, then approve from the feed.',
      }
    case 'expired':
      return { status: 400, title: 'Link expired', message: 'This one-tap link has expired. Open Kindlast to act on the finding.' }
    case 'not_found':
      return { status: 404, title: 'Finding not found', message: 'This finding no longer exists.' }
    case 'invalid':
    default:
      return { status: 400, title: 'Invalid link', message: 'This link could not be verified. Open Kindlast to act on the finding.' }
  }
}

function htmlPage({ title, message }: PageCopy): string {
  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <meta name="robots" content="noindex" />
    <title>${title} — Kindlast</title>
  </head>
  <body style="margin:0;background:#0b0b0f;color:#e5e7eb;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;">
    <div style="max-width:440px;margin:12vh auto 0;padding:32px 24px;text-align:center;">
      <h1 style="font-size:22px;color:#fafafa;margin:0 0 12px;">${title}</h1>
      <p style="font-size:15px;line-height:1.5;color:#a1a1aa;margin:0;">${message}</p>
    </div>
  </body>
</html>`
}

export async function GET(request: Request) {
  const token = new URL(request.url).searchParams.get('token') ?? ''
  const secret = process.env.NOTIFICATION_TOKEN_SECRET
  if (!secret) {
    return new Response(htmlPage({ status: 500, title: 'Not configured', message: 'Action links are unavailable.' }), {
      status: 500,
      headers: { 'content-type': 'text/html; charset=utf-8' },
    })
  }

  const result = await performFindingAction({ supabase: createServiceRoleClient(), token, secret })
  const copy = pageCopy(result)
  return new Response(htmlPage(copy), {
    status: copy.status,
    headers: { 'content-type': 'text/html; charset=utf-8' },
  })
}
