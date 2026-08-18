import { OUTCOME_LABELS, type Fetch } from '@/lib/integrations/client'

/**
 * What we fetched, including what we declined to fetch (ENT-231).
 *
 * # THE DECLINED ROWS ARE THE HALF WORTH SHOWING
 *
 * A log holding only successful fetches is indistinguishable from a deployment
 * where the policy gateway does nothing. "We did not call close_ticket because
 * this connection has not granted write access" is the sentence that makes the
 * control visible, and it can only appear here.
 *
 * So `refused` reads as "Declined" rather than as an error, and it is not
 * coloured like a failure. A customer seeing red beside every refusal would
 * conclude the product is broken rather than that it did what they asked it
 * to do.
 *
 * # THE REDACTION COUNT IS SHOWN WHEN IT IS NOT ZERO
 *
 * Because "we removed three values before storing this" is a promise being
 * kept in public. Zero is not shown, since a row saying "0 values removed" on
 * every line is noise that would make the non-zero ones harder to spot.
 */
export function FetchList({ fetches }: { fetches: Fetch[] }) {
  return (
    <ul className="mt-3 divide-y divide-border/60 rounded-xl border border-border/60 bg-background">
      {fetches.map((fetch) => (
        <li key={fetch.id} className="p-4">
          <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
            <p className="text-sm font-medium text-foreground">
              <span className="font-mono">{fetch.tool}</span>
              {fetch.integrationName ? (
                <span className="ml-2 font-normal text-muted-foreground">
                  on {fetch.integrationName}
                </span>
              ) : null}
            </p>
            <p className="text-xs text-muted-foreground">
              {fetch.outcome
                ? (OUTCOME_LABELS[fetch.outcome] ?? fetch.outcome)
                : null}
            </p>
          </div>

          {fetch.requestedAt ? (
            <p className="mt-1 text-xs text-muted-foreground">
              <time dateTime={fetch.requestedAt}>
                {formatMoment(fetch.requestedAt)}
              </time>
            </p>
          ) : null}

          {/*
            core-api's own sentence, passed through. It is written for a person
            and it is the specific one: replacing it with something vaguer here
            would leave a customer unable to tell a tool they have not granted
            from a host their operator has not permitted.
          */}
          {fetch.detail ? (
            <p className="mt-1 text-xs text-muted-foreground">{fetch.detail}</p>
          ) : null}

          {fetch.redactions && fetch.redactions > 0 ? (
            <p className="mt-1 text-xs text-muted-foreground">
              {fetch.redactions === 1
                ? '1 value was removed before this was stored'
                : `${fetch.redactions} values were removed before this was stored`}
            </p>
          ) : null}
        </li>
      ))}
    </ul>
  )
}

function formatMoment(iso: string): string {
  const parsed = new Date(iso)
  if (Number.isNaN(parsed.getTime())) return iso
  return parsed.toLocaleString('en-GB', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}
