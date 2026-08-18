import Link from 'next/link'

import { SeverityBadge, StatusLabel } from '@/components/feed/severity'
import { orgPath } from '@/lib/auth/org'
import type { Citation, Finding } from '@/lib/findings/client'

/**
 * One finding in the feed (ENT-203, ENT-162, ENT-164).
 *
 * # THE HEADING IS `detected` AND THE NARRATIVE IS BODY COPY
 *
 * `detected` is what the Watcher observed, in the customer's terms, and it is a
 * short phrase. The narrative is one or two sentences the Analyst drafted, and
 * it goes underneath. ENT-164 is what happens when those two share a column:
 * the old narrative layer wrote prose over `detected`, so a card whose heading
 * was a phrase became a heading three lines long. 00022 gave the prose its own
 * column and this is the reading half of the same fix.
 *
 * # ONE BODY SLOT, AND THE NARRATIVE WINS IT
 *
 * The card has room for the heading and one line of body, which today holds the
 * proposed action. When a narrative exists it takes that slot instead, because
 * the baseline proposed action is the same sentence on every card and that
 * sameness is literally what ENT-162 was filed about. The finding page shows
 * both in full; the card shows whichever of the two actually distinguishes this
 * finding from the one under it.
 *
 * A card with no narrative therefore renders exactly what it rendered before
 * any of this existed. That is the case to protect rather than the exception:
 * narration is an asynchronous job and Intelligence is an optional deployment
 * profile, so most cards will never have one, and an empty box or a "narrative
 * pending" line would turn the ordinary state into a defect report.
 */
export function FindingCard({
  finding,
  orgSlug,
}: {
  finding: Finding
  orgSlug: string
}) {
  return (
    <li>
      <Link
        href={orgPath(orgSlug, `/feed/${finding.findingId}`)}
        className="block rounded-xl border border-border/60 bg-background p-4 transition-colors hover:border-border hover:bg-muted/40"
      >
        <div className="flex flex-wrap items-center gap-2">
          <SeverityBadge severity={finding.severity} />
          <StatusLabel status={finding.status} />
        </div>

        {/* A heading element rather than a styled paragraph, so "this is the
            heading" is something the markup states and a test can hold us to,
            rather than a convention the next narrative layer can quietly
            break. */}
        <h3 className="mt-2 text-[15px] font-medium text-foreground">
          {finding.detected}
        </h3>

        {finding.narrative ? (
          <>
            <p
              data-testid="finding-narrative"
              className="mt-1 line-clamp-2 text-sm text-muted-foreground"
            >
              {finding.narrative}
            </p>
            {/* GENERATED PROSE IS NEVER UNMARKED (ENT-248).

                The card has no room for the authored statement of law, and the
                finding page carries it. What the card owes the reader is the
                one fact they cannot recover by looking: that this sentence was
                drafted rather than written. A customer told the difference can
                weigh it, and one not told cannot, and Article 50 of the AI Act
                wants the AI-generated nature disclosed regardless.

                It is small and quiet on purpose. This is a list of the
                customer's compliance gaps, and a prominent banner about our
                pipeline would be a line about us in a place reserved for
                them. */}
            <p className="mt-1 text-[11px] tracking-[0.04em] text-muted-foreground/80 uppercase">
              Drafted by the Analyst
            </p>
          </>
        ) : finding.proposedAction ? (
          <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
            {finding.proposedAction}
          </p>
        ) : null}

        <CitationLine citation={finding.citation} />
      </Link>
    </li>
  )
}

/**
 * The narrative on the finding page, in full, with who wrote it (ENT-162).
 *
 * # WHY THE REFUSAL IS HERE AND NOT ON THE CARD
 *
 * A refusal means a run was made and the guardrail ring rejected what came
 * back, usually because the draft cited an article this finding is not about.
 * It is worth showing: a refusal that leaves no trace is indistinguishable from
 * never having run, and a customer deciding whether to trust a compliance
 * product should be able to tell those apart.
 *
 * It is worth showing HERE. The feed is a list of the customer's compliance
 * gaps, and a card on it saying what our pipeline could not do is a line about
 * us in a place reserved for them. Somebody who has opened one finding is
 * asking about that finding, and this is the answer to "is there more, and did
 * anything try".
 *
 * # THE RUN IS NAMED, NOT LINKED
 *
 * `agent_runs` has a write path (`IngestService.RecordAgentRun`) and no read
 * path, so there is no page for a run to link to. ENT-232 proposed adding one.
 * Until it exists the id is shown as the reference it is, which is quotable in
 * a support conversation, rather than as a link to nowhere.
 */
export function FindingNarrative({ finding }: { finding: Finding }) {
  const summary = finding.citation?.obligationSummary

  if (!finding.narrative && !finding.narrativeRefusal && !summary) return null

  return (
    <section className="mt-6">
      {finding.narrative || finding.narrativeRefusal ? (
        <>
          <h2 className="text-xs font-medium tracking-[0.08em] text-muted-foreground uppercase">
            What this means for you
          </h2>

          {finding.narrative ? (
            <p
              data-testid="finding-narrative"
              className="mt-2 text-[15px] text-foreground"
            >
              {finding.narrative}
            </p>
          ) : (
            <p
              data-testid="finding-narrative-refusal"
              className="mt-2 text-sm text-muted-foreground"
            >
              The Analyst tried to explain this one and its draft was refused:{' '}
              {finding.narrativeRefusal}. Nothing else on this page was written
              by a model, so nothing else changed when that happened.
            </p>
          )}

          <p className="mt-2 text-xs text-muted-foreground">
            {finding.narrative
              ? 'Drafted by the Analyst about your organisation, not a statement of the law'
              : 'Attempted by the Analyst'}
            {finding.agentRunId ? (
              <>
                , run <span className="font-mono">{finding.agentRunId}</span>
              </>
            ) : null}
            .
          </p>
        </>
      ) : null}

      {summary ? <ObligationStatement summary={summary} /> : null}
    </section>
  )
}

/**
 * The authored statement of the law, beside the generated one (ENT-248).
 *
 * # WHY THIS SITS HERE AND NOT ONLY UNDER "THE REGULATION"
 *
 * Two live narrations on the 2B tier cited Article 30 correctly and stated
 * Article 30(5) backwards in the prose beside the citation. That failure is
 * invisible to the check a careful customer performs: they follow the citation,
 * find it valid, and believe the sentence next to it.
 *
 * Two things answer it. The model is no longer asked to state the law, in the
 * skill's schema and in a critic that refuses a draft which does anyway. And
 * the statement of law reaches the same eye-line from the corpus row, so a
 * reader comparing the two paragraphs is comparing what a person wrote against
 * what a model drafted, rather than reading one paragraph and trusting it.
 *
 * Putting it further down the page under "The regulation" would technically
 * show it. It would not be beside anything, and the whole value is adjacency.
 *
 * # VERBATIM, AND THAT IS THE POINT
 *
 * Rendered exactly as stored. No truncation, no clamping, no "read more". A
 * summary of the summary is a second author, and there is exactly one author of
 * a statement of law in this product.
 */
function ObligationStatement({ summary }: { summary: string }) {
  return (
    <div className="mt-5 border-l-2 border-border/60 pl-4">
      <h3 className="text-xs font-medium tracking-[0.08em] text-muted-foreground uppercase">
        What the regulation says
      </h3>
      <p
        data-testid="obligation-summary"
        className="mt-2 text-[15px] text-foreground"
      >
        {summary}
      </p>
      <p className="mt-2 text-xs text-muted-foreground">
        Written by a person from the regulation text, and not generated.
      </p>
    </div>
  )
}

/**
 * The citation, as stored.
 *
 * Renders `label` and nothing else. It is not assembled here from `celex` and
 * `article`, and it must not be: a second assembler is a second thing that can
 * disagree with what the Analyst recorded, and the product's whole claim is
 * that a human can check this against the law.
 *
 * A finding with no stored label shows nothing rather than a guess. Silence is
 * recoverable; an invented citation is the one failure this product cannot
 * afford.
 */
export function CitationLine({ citation }: { citation?: Citation }) {
  if (!citation?.label) return null

  return (
    <p className="mt-3 font-mono text-xs text-muted-foreground">
      {citation.label}
    </p>
  )
}
