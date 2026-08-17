/**
 * The regulatory corpus, from web's side (ENT-207).
 *
 * Read only, and there is no write call because there is no write RPC. Writing
 * the corpus is `IngestService` on the platform surface, behind
 * `internal:ingest`, on a Postgres role that holds grants on the ten regulatory
 * tables and cannot read a finding.
 *
 * That separation is the point rather than tidiness: a finding says an
 * organisation has an obligation and cites the law it comes from, and the
 * reason to believe it is that the thing serving the claim cannot edit the law.
 */
import { call } from '@/lib/core-api/call'

export type { Failure, Result } from '@/lib/core-api/call'

export interface Citation {
  obligationSlug?: string
  title?: string
  celex?: string
  kind?: string
  article?: number
  recital?: number
  annex?: string
  paragraph?: string
  /** Rendered, e.g. `GDPR Art. 30`. Render this; do not rebuild it. A label
   *  reading like `32016R0679 Art. 30` means the corpus has no document under
   *  that CELEX and the helper fell back. */
  label?: string
  /** Deep link to the provision on EUR-Lex. May be empty, and then a client
   *  shows the label unlinked rather than guessing a URL. */
  url?: string
}

export interface CorpusObligation {
  slug?: string
  title?: string
  summary?: string
  severity?: string
  recurrence?: string
  dueWithinDays?: number
  effectiveDate?: string
  topicTags?: string[]
  actionType?: string
  citation?: Citation
}

export interface RegulatoryDocumentSummary {
  celexNumber?: string
  title?: string
  shortTitle?: string
  versionDate?: string
  officialUrl?: string
  articleCount?: number
  recitalCount?: number
  annexCount?: number
}

export function listObligations(accessToken: string, orgId: string) {
  return call<{ obligations?: CorpusObligation[] }>(
    'kindlast.core.v1.CorpusService/ListObligations',
    { accessToken, orgId },
  )
}

export function getObligation(
  accessToken: string,
  orgId: string,
  slug: string,
) {
  return call<{
    obligation?: CorpusObligation
    citedSummary?: string
    citedHeading?: string
    citedParagraphSummary?: string
  }>('kindlast.core.v1.CorpusService/GetObligation', {
    accessToken,
    orgId,
    body: { slug },
  })
}

export function listDocuments(accessToken: string, orgId: string) {
  return call<{ documents?: RegulatoryDocumentSummary[] }>(
    'kindlast.core.v1.CorpusService/ListDocuments',
    { accessToken, orgId },
  )
}
