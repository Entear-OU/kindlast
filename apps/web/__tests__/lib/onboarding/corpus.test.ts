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
} from '@/lib/onboarding/corpus'
import { LAWFUL_BASIS_LABELS } from '@/lib/onboarding/answers'
import { LEGAL_ASSERTION_PATTERNS, assertsLaw } from '@/lib/onboarding/claims'

/**
 * ENT-189 and ENT-254, holding ENT-248's ruling on the surface where a customer
 * meets the product's claims first.
 *
 * The ruling: the statement of law comes from the corpus, and free text written
 * about the organisation may not assert law. It was settled after the local
 * model produced a narrative that cited Article 30 correctly and stated the
 * opposite of Article 30(5) beside it. A citation validator cannot catch that,
 * because the citation was right.
 *
 * The guard is structural rather than editorial, in two halves:
 *
 *  1. Every sentence of law the assessment renders is byte-identical to a
 *     `summary` in `data/corpus/obligations.json`. There is no second copy in
 *     `apps/web` that could drift.
 *  2. Every sentence the assessment writes ITSELF is run past the same detector
 *     `apps/intelligence`'s code critic uses, and must come back clean.
 *
 * THE QUESTION PROMPTS MOVED, AND SO DID THEIR HALF OF THIS TEST. Until ENT-254
 * the script lived in `lib/readiness/script.ts` and the walk over it was here.
 * The questions are core-api's now, so the same detector walks them from Go, in
 * `apps/core-api/internal/domain/onboarding/script_test.go`, using
 * `internal/domain/claims`. What stays here is everything still authored in
 * TypeScript: `copy.ts` (its own file) and `evaluate.ts`'s sentences about the
 * organisation (`evaluate.test.ts`).
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
  it('asserts no law in any lawful-basis label', () => {
    for (const label of Object.values(LAWFUL_BASIS_LABELS)) {
      expect(assertsLaw(label), label).toBe(false)
    }
  })

  it('can name every lawful basis the corpus narrows on', () => {
    // `evaluate.ts` explains why Article 7 reached somebody by naming the
    // ground they said they rely on, in words. An obligation narrowing on a
    // basis this table does not hold would produce a sentence with a token in
    // it, on the one page whose promise is that the result is checkable.
    for (const obligation of OBLIGATIONS) {
      const basis = obligation.appliesWhen?.lawful_basis_includes
      if (!basis) continue
      expect(Object.keys(LAWFUL_BASIS_LABELS), basis).toContain(basis)
    }
  })
})
