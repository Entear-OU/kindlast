/**
 * The corpus, as the public readiness assessment reads it (ENT-189).
 *
 * # THE STATEMENT OF LAW IS IMPORTED, NEVER WRITTEN HERE
 *
 * ENT-248 settled the rule this file exists to hold: what the law says comes
 * from a corpus row, and what is said about the organisation is written
 * separately and asserts nothing legal. The reason is concrete rather than
 * stylistic. Driving the narrator against the real model produced a narrative
 * that cited Article 30 correctly and stated the opposite of Article 30(5)
 * beside it, and no citation validator can catch that, because the citation
 * was right.
 *
 * On the marketing site the same mistake is worse. The reader has no account,
 * no compliance record to check the claim against, and no reason yet to doubt
 * us. So this module imports `data/corpus/obligations.json` and the two
 * regulation packs directly, and every sentence of law the assessment renders
 * is a `summary` string out of those files, unedited. There is no second copy
 * of the text in `apps/web` for somebody to "improve" for the web, and
 * `__tests__/lib/readiness/corpus.test.ts` fails if one appears.
 *
 * # WHY AN IMPORT AND NOT AN RPC
 *
 * `CorpusService` would be the obvious source, and it is the wrong one here:
 * every RPC on it needs a bearer token, and this surface has no session by
 * design. Reaching core-api unauthenticated would mean opening a public read
 * path into the resource server for a marketing page, which is a security
 * boundary moved for a conversion action.
 *
 * The JSON is not a copy of the corpus, it IS the corpus: `corpus-load` ingests
 * these same files, and `corpus_drift_test.go` asserts the rows read back match
 * them. Importing at build time gets the identical text with no request, no
 * token, and nothing to rate limit.
 */
import aiAct from '../../../../data/corpus/eu-ai-act.json'
import gdpr from '../../../../data/corpus/gdpr.json'
import obligations from '../../../../data/corpus/obligations.json'

/** CELEX numbers, spelled as the packs spell them. */
export const GDPR_CELEX = '32016R0679'
export const AI_ACT_CELEX = '32024R1689'

export type Celex = typeof GDPR_CELEX | typeof AI_ACT_CELEX

/**
 * Who an obligation binds. GDPR's actor is the controller, the AI Act's are
 * the deployer and the provider, and they sit in one flat list because that is
 * how `domain/corpus/applieswhen.go` declares them.
 */
export type Role = 'controller' | 'deployer' | 'provider'

/** The `requires` vocabulary: a control whose absence is a gap. */
export type GapToken = 'ropa' | 'dpo' | 'ai_register' | 'transfer_safeguards'

export type ThresholdKey =
  | 'cross_border_transfers'
  | 'high_risk_processing'
  | 'high_risk_ai_system'
  | 'large_scale_monitoring'

export interface AppliesWhen {
  role?: Role
  requires?: GapToken[]
  thresholds?: Partial<Record<ThresholdKey, boolean>>
  engages_processor?: boolean
  lawful_basis_includes?: string
}

export interface Citation {
  kind: 'article' | 'recital' | 'annex'
  celex: string
  articleNumber?: number
  annexLabel?: string
}

export interface Obligation {
  slug: string
  title: string
  /** The plain-language statement of law, verbatim from the pack. */
  summary: string
  citation: Citation
  appliesWhen?: AppliesWhen
  severity: string
  recurrence: string
  topicTags?: string[]
}

/**
 * The regulations the obligations cite, keyed by CELEX.
 *
 * `shortTitle` and `officialUrl` are bibliographic facts read from the packs
 * rather than retyped, so a citation on this page resolves to the same official
 * text a finding in the product resolves to.
 */
export const REGULATIONS: Record<
  string,
  { shortTitle: string; officialUrl: string; label: string }
> = {
  [GDPR_CELEX]: {
    shortTitle: gdpr.document.shortTitle,
    officialUrl: gdpr.document.officialUrl,
    label: 'GDPR',
  },
  [AI_ACT_CELEX]: {
    shortTitle: aiAct.document.shortTitle,
    officialUrl: aiAct.document.officialUrl,
    label: 'EU AI Act',
  },
}

/**
 * Every obligation in the corpus, in pack order.
 *
 * Cast rather than validated at runtime, deliberately. The file is a build
 * input, not a request payload, and it is already validated where validation
 * belongs: `IngestCorpus` refuses an `appliesWhen` token outside the
 * vocabulary, naming the token and the vocabulary. A second validator here
 * would be a second opinion about the same data with no way to disagree
 * usefully.
 */
export const OBLIGATIONS: readonly Obligation[] =
  obligations.obligations as readonly Obligation[]

/** "Article 30 GDPR", "Annex III EU AI Act". */
export function citationLabel(citation: Citation): string {
  const regulation = REGULATIONS[citation.celex]?.label ?? citation.celex
  if (citation.kind === 'annex' && citation.annexLabel) {
    return `Annex ${citation.annexLabel} ${regulation}`
  }
  if (citation.kind === 'recital' && citation.articleNumber !== undefined) {
    return `Recital ${citation.articleNumber} ${regulation}`
  }
  if (citation.articleNumber !== undefined) {
    return `Article ${citation.articleNumber} ${regulation}`
  }
  return regulation
}

/** Where a reader goes to check the claim against the official text. */
export function citationUrl(citation: Citation): string | undefined {
  return REGULATIONS[citation.celex]?.officialUrl
}

export function obligationBySlug(slug: string): Obligation | undefined {
  return OBLIGATIONS.find((o) => o.slug === slug)
}
