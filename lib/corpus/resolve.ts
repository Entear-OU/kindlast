/**
 * Citation resolver (ENT-98).
 *
 * Bridges the corpus catalog (natural keys) and the runtime fetch path
 * (a `WebSearchProvider`). Given a `CitationKey`, the resolver constructs
 * the EUR-Lex ELI anchor URL and asks the provider to fetch it. The
 * Analyst calls `fetchCitation(key)` and gets verbatim OJ text back —
 * the architectural endpoint of progressive disclosure (ENT-32).
 *
 * Why ELI anchors:
 *   * Deterministic per regulation — no per-row URL column needed in the
 *     corpus schema.
 *   * Stable across mirror sites — EUR-Lex's ELI URI format is part of the
 *     OJ's permanent identifier scheme.
 *   * Deep-linked: `#art_6`, `#rct_47`, `#anx_III` jump straight to the
 *     element. Useful in the audit trail and for direct human review.
 *
 * Annex items don't have first-class deep links in EUR-Lex's ELI — the
 * Analyst fetches the annex page and the LLM disambiguates the item from
 * the catalog `item_label` + the fetched content. We accept that
 * trade-off; the alternative is per-row URLs in the schema, which we
 * explicitly ruled out under progressive disclosure.
 */

import { getWebSearchProvider } from '@/lib/websearch'
import type {
  FetchUrlOptions,
  FetchUrlResult,
  WebSearchProvider,
} from '@/lib/websearch/types'

export type CitationKey =
  | {
      kind: 'article'
      celex: string
      articleNumber: number
      /**
       * Optional paragraph label for sub-paragraph rows (e.g. "1(a)"). Not
       * encoded in the URL because EUR-Lex does not deep-link to sub-paragraphs;
       * the LLM uses this label when scanning the fetched article text.
       */
      paragraphLabel?: string
    }
  | { kind: 'recital'; celex: string; recitalNumber: number }
  | {
      kind: 'annex'
      celex: string
      annexLabel: string
      /** Optional annex item label — passed through to the LLM, not the URL. */
      itemLabel?: string
    }

const CELEX_RE = /^3(\d{4})R(\d{4})$/

function eliBase(celex: string): string {
  const match = celex.match(CELEX_RE)
  if (!match) {
    throw new Error(
      `citationKeyToUrl: CELEX ${celex} is not a recognised regulation identifier (expected pattern 3{year}R{number}, e.g. 32016R0679)`,
    )
  }
  const [, year, number] = match
  return `https://eur-lex.europa.eu/eli/reg/${year}/${Number(number)}/oj`
}

export function citationKeyToUrl(key: CitationKey): string {
  switch (key.kind) {
    case 'article': {
      if (!Number.isInteger(key.articleNumber) || key.articleNumber <= 0) {
        throw new Error(`citationKeyToUrl: articleNumber must be a positive integer`)
      }
      return `${eliBase(key.celex)}#art_${key.articleNumber}`
    }
    case 'recital': {
      if (!Number.isInteger(key.recitalNumber) || key.recitalNumber <= 0) {
        throw new Error(`citationKeyToUrl: recitalNumber must be a positive integer`)
      }
      return `${eliBase(key.celex)}#rct_${key.recitalNumber}`
    }
    case 'annex': {
      if (!key.annexLabel) {
        throw new Error(`citationKeyToUrl: annexLabel is required for annex citations`)
      }
      return `${eliBase(key.celex)}#anx_${key.annexLabel}`
    }
  }
}

export async function fetchCitation(
  key: CitationKey,
  provider?: WebSearchProvider,
  options?: FetchUrlOptions,
): Promise<FetchUrlResult> {
  const url = citationKeyToUrl(key)
  const p = provider ?? getWebSearchProvider()
  return p.fetchUrl(url, options)
}
