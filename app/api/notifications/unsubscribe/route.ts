import { performUnsubscribe, type UnsubscribeResultKind } from '@/lib/notifications/unsubscribe'
import { createServiceRoleClient } from '@/lib/supabase/service-role'

/**
 * Weekly-briefing unsubscribe endpoint (ENT-74).
 *
 * The "Stop weekly briefings" link in a briefing footer links here with a signed
 * token. A GET (clicked from a mail client), it verifies the token, flips the
 * preference via the service role, and returns a small branded HTML page.
 */

export const dynamic = 'force-dynamic'

interface PageCopy {
  status: number
  title: string
  message: string
}

function pageCopy(kind: UnsubscribeResultKind): PageCopy {
  switch (kind) {
    case 'ok':
      return {
        status: 200,
        title: 'Unsubscribed',
        message: "You won't receive the weekly compliance briefing anymore. You can re-enable it anytime in Kindlast settings.",
      }
    case 'expired':
      return { status: 400, title: 'Link expired', message: 'This unsubscribe link has expired. Manage briefings from Kindlast settings.' }
    case 'invalid':
    default:
      return { status: 400, title: 'Invalid link', message: 'This link could not be verified. Manage briefings from Kindlast settings.' }
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
    return new Response(htmlPage({ status: 500, title: 'Not configured', message: 'Unsubscribe links are unavailable.' }), {
      status: 500,
      headers: { 'content-type': 'text/html; charset=utf-8' },
    })
  }

  const kind = await performUnsubscribe({ supabase: createServiceRoleClient(), token, secret })
  const copy = pageCopy(kind)
  return new Response(htmlPage(copy), {
    status: copy.status,
    headers: { 'content-type': 'text/html; charset=utf-8' },
  })
}
