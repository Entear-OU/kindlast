import { readFileSync } from 'node:fs'
import path from 'node:path'

import { describe, expect, it } from 'vitest'

import {
  AI_ACT_CELEX,
  GDPR_CELEX,
  OBLIGATIONS,
  REGULATIONS,
  citationLabel,
  citationUrl,
} from '@/lib/readiness/corpus'
import { SCRIPT } from '@/lib/readiness/script'
import { LEGAL_ASSERTION_PATTERNS, assertsLaw } from '@/lib/readiness/claims'

/**
 * ENT-189, holding ENT-248's ruling on the one surface where breaking it costs
 * the most.
 *
 * The ruling: the statement of law comes from the corpus, and free text written
 * about the organisation may not assert law. It was settled after the local
 * model produced a narrative that cited Article 30 correctly and stated the
 * opposite of Article 30(5) beside it. A citation validator cannot catch that,
 * because the citation was right.
 *
 * On the marketing site the reader has no account, no record to check it
 * against, and no reason yet to doubt us, so the guard has to be structural
 * rather than editorial. Two halves:
 *
 *  1. Every sentence of law the assessment renders is byte-identical to a
 *     `summary` in `data/corpus/obligations.json`. There is no second copy in
 *     `apps/web` that could drift.
 *  2. Every sentence the assessment writes ITSELF (the question prompts, the
 *     help text, the "why this reached you" lines) is run past the same
 *     detector `apps/intelligence`'s code critic uses, and must come back
 *     clean.
 */

const CORPUS = path.resolve(__dirname, '../../../../../data/corpus')

function readPack(file: string): unknown {
  return JSON.parse(readFileSync(path.join(CORPUS, file), 'utf8'))
}

describe('the corpus this surface reads', () => {
  it('is the file core-api ingests, not a copy of it', () => {
    const onDisk = readPack('obligations.json') as {
      obligations: Array<{ slug: string; summary: string }>
    }

    expect(OBLIGATIONS.length).toBe(onDisk.obligations.length)
    for (const [index, obligation] of OBLIGATIONS.entries()) {
      expect(obligation.slug).toBe(onDisk.obligations[index].slug)
      // Byte-identical. If somebody ever "improves the wording for the web",
      // this is the test that stops it.
      expect(obligation.summary).toBe(onDisk.obligations[index].summary)
    }
  })

  it('takes the regulation names and official links from the packs', () => {
    const gdpr = readPack('gdpr.json') as {
      document: { shortTitle: string; officialUrl: string }
    }
    const aiAct = readPack('eu-ai-act.json') as {
      document: { shortTitle: string; officialUrl: string }
    }

    expect(REGULATIONS[GDPR_CELEX].shortTitle).toBe(gdpr.document.shortTitle)
    expect(REGULATIONS[GDPR_CELEX].officialUrl).toBe(gdpr.document.officialUrl)
    expect(REGULATIONS[AI_ACT_CELEX].shortTitle).toBe(aiAct.document.shortTitle)
    expect(REGULATIONS[AI_ACT_CELEX].officialUrl).toBe(
      aiAct.document.officialUrl,
    )
  })

  it('gives every obligation a citation a reader can follow', () => {
    for (const obligation of OBLIGATIONS) {
      expect(citationLabel(obligation.citation)).toMatch(/GDPR|EU AI Act/)
      expect(citationUrl(obligation.citation)).toMatch(/^https:\/\/eur-lex\./)
    }
  })

  it('renders an annex citation as an annex', () => {
    expect(
      citationLabel({ kind: 'annex', celex: AI_ACT_CELEX, annexLabel: 'III' }),
    ).toBe('Annex III EU AI Act')
    expect(
      citationLabel({ kind: 'article', celex: GDPR_CELEX, articleNumber: 30 }),
    ).toBe('Article 30 GDPR')
  })
})

describe('the detector itself', () => {
  // A guard that cannot go red reports a safety that is not there, so the
  // detector is proved against sentences that are unambiguously statements of
  // law before it is trusted to clear anything.
  it('catches the shapes the code critic catches', () => {
    expect(assertsLaw('Article 30 requires a written record.')).toBe(true)
    expect(assertsLaw('This applies to every controller.')).toBe(true)
    expect(
      assertsLaw('Controllers must appoint a data protection officer.'),
    ).toBe(true)
    expect(assertsLaw('Small companies are exempt from this.')).toBe(true)
    expect(assertsLaw('It applies regardless of headcount.')).toBe(true)
    expect(assertsLaw('See Recital 47 for the reasoning.')).toBe(true)
    expect(assertsLaw('The GDPR requires notification within 72 hours.')).toBe(
      true,
    )
  })

  it('clears a sentence that is only about the organisation', () => {
    expect(assertsLaw('You said personal information leaves the EU.')).toBe(
      false,
    )
    expect(assertsLaw('You told us you keep no record of what you do.')).toBe(
      false,
    )
    expect(assertsLaw('Nothing you told us narrows this one.')).toBe(false)
  })

  it('allows the second person, exactly as the Python critic does', () => {
    // "You" is this organisation, which is what a page written for a visitor is
    // entitled to talk about. "Controllers must" is the corpus's sentence.
    expect(assertsLaw('You told us you have not appointed anybody.')).toBe(
      false,
    )
  })

  it('declares at least one pattern per shape it claims to detect', () => {
    expect(LEGAL_ASSERTION_PATTERNS.length).toBeGreaterThanOrEqual(5)
  })
})

describe('everything this surface writes for itself', () => {
  it('asserts no law in any question the assessment asks', () => {
    for (const question of SCRIPT) {
      expect(assertsLaw(question.prompt), question.prompt).toBe(false)
      if (question.help) {
        expect(assertsLaw(question.help), question.help).toBe(false)
      }
      if (question.kind === 'multi') {
        for (const option of question.options) {
          expect(assertsLaw(option.label), option.label).toBe(false)
        }
      }
    }
  })
})
