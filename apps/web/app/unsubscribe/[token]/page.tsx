import { UnsubscribeForm } from '@/components/notifications/unsubscribe-form'

/**
 * Unsubscribing from a link in an email (ENT-209).
 *
 * # WHY THIS PAGE EXISTS AT ALL, RATHER THAN THE LINK JUST DOING IT
 *
 * A GET that changes something is wrong in principle and actively dangerous
 * here. Corporate mail gateways and link scanners follow every URL in a message
 * before a human ever sees it, so a one-click GET would unsubscribe people by
 * the act of delivering the email to them. The symptom is a customer who
 * quietly stops receiving compliance notifications, for a reason nobody can
 * reconstruct months later.
 *
 * So the link renders this, and the button posts.
 *
 * # WHY NOTHING IS VALIDATED BEFORE THE BUTTON
 *
 * The token is not checked on the way in, deliberately, and this page looks the
 * same whether it is real, expired, already used or invented. Checking early
 * would mean an unauthenticated endpoint that reports whether a given token
 * exists, and this page would be the thing serving that oracle. The answer
 * comes at the point of redemption, where it is one answer for every unusable
 * token.
 *
 * There is no session here and there must not be one required. Somebody trying
 * to stop mail they did not ask for should never be asked to sign in first:
 * that is how a product earns a spam complaint instead of an unsubscribe, which
 * costs the sending domain's reputation rather than one message.
 */
export default async function UnsubscribePage({
  params,
}: {
  params: Promise<{ token: string }>
}) {
  const { token } = await params

  return (
    <main className="mx-auto w-full max-w-md px-4 py-20">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        Stop these emails?
      </h1>
      <p className="mt-3 text-sm text-muted-foreground">
        This turns off the weekly briefing and deadline alerts for you, and
        limits finding emails to critical ones only. It applies to the
        organisation this email came from and changes nothing for anyone else.
      </p>
      <p className="mt-3 text-sm text-muted-foreground">
        You can turn any of it back on from the settings page whenever you like.
      </p>

      <div className="mt-8">
        <UnsubscribeForm token={token} />
      </div>
    </main>
  )
}
