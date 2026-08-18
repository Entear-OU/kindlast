import { ApproveFromEmailForm } from '@/components/feed/approve-from-email-form'

/**
 * Approving a finding from a link in an email (§8, ENT-249).
 *
 * # WHY THIS PAGE EXISTS AT ALL, RATHER THAN THE LINK JUST DOING IT
 *
 * The same argument the unsubscribe interstitial makes, one step more serious.
 * Corporate mail gateways, link previewers and archiving proxies fetch every
 * URL in a message before a human sees it, so a one-click GET would approve
 * findings by the act of delivering the email. An unsubscribe that fires in
 * transit quietly stops somebody's mail. An approval that fires in transit
 * quietly makes a regulatory decision, writes it into the customer's own
 * compliance record, and names a person who never opened the message.
 *
 * So the link renders this, and the button posts. Rendering this page does
 * nothing at all: no fetch, no validation, no call to core-api.
 *
 * # WHY NOTHING IS CHECKED BEFORE THE BUTTON
 *
 * Deliberately, and this page looks the same whether the link is real,
 * expired, already used, minted for a different finding or invented. Checking
 * on the way in would make this an unauthenticated endpoint that reports
 * whether a given credential is live, and this page would be the thing serving
 * that oracle. The answer comes at the point of redemption, where it is one
 * answer for every unusable link.
 *
 * # WHY IT SAYS WHAT IT SAYS
 *
 * It cannot show the finding, because there is no session here and the finding
 * is the customer's compliance exposure. That is not a gap to apologise for: it
 * is the doorbell rule (§17.1) holding all the way to the last click. What the
 * page owes the reader instead is honesty about what they are about to do, so
 * it says plainly that approving from here records a decision made without
 * reading the finding, and offers the console as the alternative.
 */
export default async function ApproveFromEmailPage({
  params,
}: {
  params: Promise<{ findingId: string; token: string }>
}) {
  const { findingId, token } = await params

  return (
    <main className="mx-auto w-full max-w-md px-4 py-20">
      <h1 className="text-2xl font-semibold tracking-[-0.02em] text-foreground">
        Approve this finding?
      </h1>
      <p className="mt-3 text-sm text-muted-foreground">
        This approves the finding the email was about, in the organisation the
        email came from, and runs anything Kindlast proposed for it. The
        approval is recorded as yours.
      </p>
      <p className="mt-3 text-sm text-muted-foreground">
        You have not read the finding here, and the audit trail will say so. If
        you would rather read it first, close this and open the finding from the
        other link in the email.
      </p>
      <p className="mt-3 text-sm text-muted-foreground">
        The link works once and expires within the hour. If it has, sign in and
        approve the finding from the feed instead.
      </p>

      <div className="mt-8">
        <ApproveFromEmailForm findingId={findingId} token={token} />
      </div>
    </main>
  )
}
