import Link from 'next/link'

import type { CorpusObligation } from '@/lib/corpus/client'

/**
 * The obligations this deployment checks against (ENT-207).
 *
 * # WHY THE CITATION IS A LINK OUT AND NOT A LINK IN
 *
 * It goes to EUR-Lex, not to a page of ours. The corpus stores no verbatim
 * Official Journal text: each row carries a curated summary, and the wording
 * lives with the publisher. That is the design rather than a gap, and it is the
 * stronger position for a compliance product to be in. A customer checking a
 * claim should land on the law rather than on our copy of it.
 *
 * # SEVERITY IS THE OBLIGATION'S, NOT A FINDING'S
 *
 * Worth being careful about in the copy. This page says how serious the
 * regulation treats a duty, which is a fact about the law and the same for
 * every customer. It says nothing about whether any particular organisation is
 * failing it. Blurring those two would turn a reference page into an alarm.
 */
export function ObligationList({
  obligations,
  hrefFor,
}: {
  obligations: CorpusObligation[]
  hrefFor: (slug: string) => string
}) {
  return (
    <ul className="space-y-3">
      {obligations.map((obligation) => (
        <li
          key={obligation.slug}
          className="rounded-xl border border-border/60 bg-background p-5"
        >
          <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
            <h2 className="text-base font-semibold text-foreground">
              <Link
                href={hrefFor(obligation.slug ?? '')}
                className="underline-offset-4 hover:underline"
              >
                {obligation.title}
              </Link>
            </h2>
            <SeverityTag severity={obligation.severity} />
          </div>

          <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
            {obligation.summary}
          </p>

          <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground">
            <CitationLink citation={obligation.citation} />
            {obligation.topicTags?.length ? (
              <span>{obligation.topicTags.join(', ')}</span>
            ) : null}
          </div>
        </li>
      ))}
    </ul>
  )
}

/**
 * The rendered citation, linked to the publisher.
 *
 * Rendered from `label`, never rebuilt from the parts. The label comes from
 * `analyst_citation_label`, which is the same function that named the citation
 * on every finding, so a provision reads identically wherever it appears. A
 * second implementation here would diverge the first time a regulation needed a
 * special case, and "GDPR Art. 30" on one page beside "32016R0679 Art. 30" on
 * another is how a customer decides the product does not know what it is
 * talking about.
 */
export function CitationLink({ citation }: { citation?: Citation }) {
  if (!citation?.label) return null

  if (!citation.url) {
    // Unlinked rather than guessing a URL. The corpus has no anchor for every
    // provision, and a link that 404s on a regulator's website is worse than
    // no link.
    return <span>{citation.label}</span>
  }

  return (
    <a
      href={citation.url}
      target="_blank"
      rel="noreferrer noopener"
      className="underline underline-offset-4"
    >
      {citation.label}
    </a>
  )
}

type Citation = NonNullable<CorpusObligation['citation']>

function SeverityTag({ severity }: { severity?: string }) {
  if (!severity) return null

  // Colour carries no information a screen reader would miss: the word is the
  // label. An icon-only or colour-only severity is the accessibility mistake
  // this kind of list makes most often.
  const tone =
    severity === 'high'
      ? 'border-destructive/40 text-destructive'
      : severity === 'medium'
        ? 'border-border text-foreground'
        : 'border-border/60 text-muted-foreground'

  return (
    <span
      className={`rounded-full border px-2 py-0.5 text-xs font-medium ${tone}`}
    >
      {severity}
    </span>
  )
}
