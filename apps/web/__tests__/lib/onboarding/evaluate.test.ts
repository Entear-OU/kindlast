import { describe, expect, it } from 'vitest'

import { OBLIGATIONS } from '@/lib/onboarding/corpus'
import {
  assess,
  gapSatisfied,
  ledger,
  ledgerCounts,
  obligationApplies,
} from '@/lib/onboarding/evaluate'
import { UNSURE, answersFrom, type Answers } from '@/lib/onboarding/answers'

/**
 * ENT-189, ENT-254. The assessment decides which obligations reach an
 * organisation, and it has to decide it the same way the product does.
 *
 * These tests are written against the semantics of `watcher_obligation_applies`
 * and `watcher_gap_satisfied` (db/migrations/00001 and 00023), not against
 * whatever this module happens to do. Where the two would disagree the test is
 * on the database's side, because a column that resolves an obligation while
 * somebody is answering is a promise about what the Watcher will say an hour
 * later.
 *
 * WHAT MOVED OUT OF THIS FILE. The question set, its options and its branching
 * were `lib/readiness/script.ts` and are core-api's since ENT-254, so the tests
 * that walked them are in
 * `apps/core-api/internal/domain/onboarding/script_test.go`. Two of them are
 * worth knowing by name because their absence here would look like a gap: the
 * one asserting the interview asks about every fact these rules read, and the
 * one asserting a question's `basis` resolves to an obligation the corpus
 * holds.
 */

/** An organisation that has answered nothing. */
const NOTHING: Answers = {}

/** A small European SaaS, answered plausibly. */
function typicalStartup(): Answers {
  return {
    data_categories: ['contact_details', 'payment', 'behaviour'],
    lawful_bases: ['contract', 'legitimate_interests'],
    vendor_list: ['hosting', 'payments', 'email'],
    transfers_outside_eu: 'yes',
    transfer_destinations: ['united_states'],
    high_risk_processing: 'no',
    large_scale_monitoring: 'no',
    has_ropa: 'no',
    has_dpo: 'no',
    ai_systems: ['assistants'],
    high_risk_ai_system: 'no',
  }
}

function bySlug(slug: string) {
  const obligation = OBLIGATIONS.find((o) => o.slug === slug)
  if (!obligation) throw new Error(`no obligation ${slug} in the corpus`)
  return obligation
}

describe('obligationApplies, ported from watcher_obligation_applies', () => {
  it('lets an obligation with no narrowing condition through', () => {
    expect(obligationApplies(bySlug('gdpr-art-6-lawful-basis'), NOTHING)).toBe(
      true,
    )
  })

  it('gates deployer and provider obligations on an AI system being in use', () => {
    const transparency = bySlug('ai-act-art-50-transparency')
    expect(obligationApplies(transparency, NOTHING)).toBe(false)
    expect(
      obligationApplies(transparency, { ...NOTHING, ai_systems: ['none'] }),
    ).toBe(false)
    expect(
      obligationApplies(transparency, {
        ...NOTHING,
        ai_systems: ['assistants'],
      }),
    ).toBe(true)
  })

  it('treats "I am not sure which AI is in use" as AI being in use', () => {
    // Not a rounding of yes. The Watcher's gate is whether the register is
    // empty, and an organisation that cannot say what it runs has not got one.
    expect(
      obligationApplies(bySlug('ai-act-art-50-transparency'), {
        ...NOTHING,
        ai_systems: [UNSURE],
      }),
    ).toBe(true)
  })

  it('requires a definite yes for cross-border transfers, and unsure is not one', () => {
    // `p_profile.transfers_outside_eu is distinct from 'yes'` in 00001, which is
    // deliberately stricter than the three fact-backed thresholds below. The
    // asymmetry is the database's and this port keeps it.
    const transfers = bySlug('gdpr-chapter-v-international-transfers')
    expect(
      obligationApplies(transfers, { ...NOTHING, transfers_outside_eu: 'yes' }),
    ).toBe(true)
    expect(
      obligationApplies(transfers, {
        ...NOTHING,
        transfers_outside_eu: 'unsure',
      }),
    ).toBe(false)
    expect(
      obligationApplies(transfers, { ...NOTHING, transfers_outside_eu: 'no' }),
    ).toBe(false)
  })

  it('counts unsure as applying for a fact-backed threshold', () => {
    // watcher_fact_affirms: yes OR unsure. "We asked and they did not know" is
    // a different claim from "they said no", and an organisation that does not
    // know has not done the screening.
    const dpia = bySlug('gdpr-art-35-dpia')
    expect(
      obligationApplies(dpia, { ...NOTHING, high_risk_processing: 'unsure' }),
    ).toBe(true)
    expect(
      obligationApplies(dpia, { ...NOTHING, high_risk_processing: 'yes' }),
    ).toBe(true)
    expect(
      obligationApplies(dpia, { ...NOTHING, high_risk_processing: 'no' }),
    ).toBe(false)
  })

  it('does not assert a DPIA from silence', () => {
    // The whole point of ENT-246. An absent answer narrows the obligation away,
    // because nobody asked, so there are no grounds.
    expect(obligationApplies(bySlug('gdpr-art-35-dpia'), NOTHING)).toBe(false)
  })

  it('never lets one high-risk answer decide the other regulation', () => {
    // `high_risk` was two questions wearing one token until ENT-246 split it.
    const dpia = bySlug('gdpr-art-35-dpia')
    const annexIII = bySlug('ai-act-annex-iii-high-risk-systems')
    const answers: Answers = {
      ...NOTHING,
      ai_systems: ['built'],
      high_risk_processing: 'yes',
      high_risk_ai_system: 'no',
    }
    expect(obligationApplies(dpia, answers)).toBe(true)
    expect(obligationApplies(annexIII, answers)).toBe(false)
  })

  it('matches a lawful basis by containment, not equality', () => {
    const consent = bySlug('gdpr-art-7-consent-conditions')
    expect(
      obligationApplies(consent, {
        ...NOTHING,
        lawful_bases: ['contract', 'consent'],
      }),
    ).toBe(true)
    expect(
      obligationApplies(consent, { ...NOTHING, lawful_bases: ['contract'] }),
    ).toBe(false)
  })

  it('gates processor contracts on somebody being named', () => {
    const article28 = bySlug('gdpr-art-28-processor-contracts')
    expect(obligationApplies(article28, NOTHING)).toBe(false)
    expect(
      obligationApplies(article28, { ...NOTHING, vendor_list: ['none'] }),
    ).toBe(false)
    expect(
      obligationApplies(article28, { ...NOTHING, vendor_list: ['hosting'] }),
    ).toBe(true)
  })
})

describe('gapSatisfied, ported from watcher_gap_satisfied', () => {
  it('is satisfied by a definite yes and by nothing else', () => {
    expect(gapSatisfied('ropa', { ...NOTHING, has_ropa: 'yes' })).toBe(true)
    expect(gapSatisfied('ropa', { ...NOTHING, has_ropa: 'unsure' })).toBe(false)
    expect(gapSatisfied('ropa', { ...NOTHING, has_ropa: 'no' })).toBe(false)
    expect(gapSatisfied('ropa', NOTHING)).toBe(false)

    expect(gapSatisfied('dpo', { ...NOTHING, has_dpo: 'yes' })).toBe(true)
    expect(gapSatisfied('dpo', { ...NOTHING, has_dpo: 'unsure' })).toBe(false)
  })

  it('treats an AI register as satisfied only when no AI is in use', () => {
    expect(
      gapSatisfied('ai_register', { ...NOTHING, ai_systems: ['none'] }),
    ).toBe(true)
    expect(
      gapSatisfied('ai_register', { ...NOTHING, ai_systems: ['assistants'] }),
    ).toBe(false)
  })

  it('needs a named destination to count transfer safeguards as in place', () => {
    expect(
      gapSatisfied('transfer_safeguards', {
        ...NOTHING,
        transfer_destinations: ['united_states'],
      }),
    ).toBe(true)
    expect(
      gapSatisfied('transfer_safeguards', {
        ...NOTHING,
        transfer_destinations: [UNSURE],
      }),
    ).toBe(false)
    expect(gapSatisfied('transfer_safeguards', NOTHING)).toBe(false)
  })
})

describe('assess', () => {
  it('reaches every obligation with either a verdict or a reason', () => {
    const result = assess(typicalStartup())
    expect(result.applies.length + result.narrowed.length).toBe(
      OBLIGATIONS.length,
    )
    expect(result.total).toBe(OBLIGATIONS.length)
  })

  it('gives every applying obligation at least one sentence about the answers', () => {
    const result = assess(typicalStartup())
    expect(result.applies.length).toBeGreaterThan(0)
    for (const applied of result.applies) {
      expect(applied.because.length).toBeGreaterThan(0)
      for (const sentence of applied.because) {
        expect(sentence.trim()).not.toBe('')
      }
    }
  })

  it('gives every narrowed obligation the answer that narrowed it', () => {
    const result = assess(typicalStartup())
    expect(result.narrowed.length).toBeGreaterThan(0)
    for (const narrowed of result.narrowed) {
      expect(narrowed.reason.trim()).not.toBe('')
    }
  })

  it('raises the gaps the visitor named themselves', () => {
    const result = assess(typicalStartup())
    const ropa = result.applies.find(
      (a) => a.obligation.slug === 'gdpr-art-30-ropa',
    )
    expect(ropa?.gaps).toContain('ropa')
    expect(ropa?.gapNotes.length).toBeGreaterThan(0)
  })

  it('writes every gap about the visitor, and implies no record of them', () => {
    // The page's promise is that it writes nothing down, so a gap sentence
    // claiming Kindlast holds something about this visitor contradicts the
    // surface it is rendered on. The `ai_register` note used to read "Kindlast
    // has nothing written down about which systems those are", which both
    // implied a record and put the gap on us rather than on them.
    //
    // A sheet answered so that every gap token fires at once, so this covers
    // all four rather than whichever the typical startup happens to trip.
    const everyGap = assess({
      ...typicalStartup(),
      has_ropa: 'no',
      has_dpo: 'no',
      // Article 37 carries the `dpo` token and narrows itself on this, so
      // without it that gap never fires and the test would cover three.
      large_scale_monitoring: 'yes',
      transfers_outside_eu: 'yes',
      transfer_destinations: [],
      ai_systems: ['assistants'],
    })

    const notes = everyGap.applies.flatMap((a) => a.gapNotes)
    expect(notes.length).toBeGreaterThanOrEqual(4)
    for (const note of notes) {
      expect(note, note).toMatch(/^You /)
      expect(note, note).not.toMatch(/Kindlast/)
    }
  })

  it('raises no gap where the visitor said the control is in place', () => {
    const result = assess({ ...typicalStartup(), has_ropa: 'yes' })
    const ropa = result.applies.find(
      (a) => a.obligation.slug === 'gdpr-art-30-ropa',
    )
    expect(ropa?.gaps).toEqual([])
  })

  it('raises no gap on an obligation the corpus attaches no token to', () => {
    // Articles 12 to 22 reach everybody and carry no `requires` token, so
    // there is nothing here for the Watcher to raise. `/readiness` filled that
    // space with the visitor's own answer about their DSAR process, carefully
    // marked as theirs rather than ours; ENT-254 dropped the question because
    // it had no fact key to be written under. What must not change is that the
    // space stays empty rather than acquiring a finding nobody made.
    const result = assess(typicalStartup())
    const dsar = result.applies.find(
      (a) => a.obligation.slug === 'gdpr-arts-12-22-data-subject-rights',
    )
    expect(dsar).toBeDefined()
    expect(dsar?.gaps).toEqual([])
  })

  it('narrows everything away for a visitor who answered no to everything', () => {
    const nothingApplies: Answers = {
      data_categories: ['none'],
      lawful_bases: [],
      vendor_list: ['none'],
      transfers_outside_eu: 'no',
      transfer_destinations: [],
      high_risk_processing: 'no',
      large_scale_monitoring: 'no',
      has_ropa: 'yes',
      has_dpo: 'yes',
      ai_systems: ['none'],
      high_risk_ai_system: 'no',
    }
    const result = assess(nothingApplies)
    // The unconditional controller obligations still reach them, and that is
    // right: nothing they said narrows those. Nothing that needed a condition
    // does.
    expect(
      result.applies.every(
        (a) =>
          a.obligation.appliesWhen?.thresholds === undefined &&
          a.obligation.appliesWhen?.engages_processor === undefined &&
          a.obligation.appliesWhen?.lawful_basis_includes === undefined,
      ),
    ).toBe(true)
    expect(result.applies.every((a) => a.gaps.length === 0)).toBe(true)
  })
})

describe('the answer sheet, read out of the interview state', () => {
  it('reads a tri-state fact back as the answer that was given', () => {
    const sheet = answersFrom({
      draft: [
        {
          key: 'PROFILE_FACT_KEY_HAS_ROPA',
          value: { triState: 'TRI_STATE_UNSURE' },
        },
      ],
    })
    expect(sheet.has_ropa).toBe('unsure')
  })

  it('reads a list fact back as its tokens', () => {
    const sheet = answersFrom({
      draft: [
        {
          key: 'PROFILE_FACT_KEY_VENDOR_LIST',
          value: { list: { values: ['hosting', 'payments'] } },
        },
      ],
    })
    expect(sheet.vendor_list).toEqual(['hosting', 'payments'])
  })

  it('reads "none of these" back as the empty list, which is an answer', () => {
    // The server records `none` as `[]`, because both evaluators decide "is
    // any AI in use" by asking whether the list holds anything. A sheet that
    // turned it back into a token would reopen every AI Act obligation for an
    // organisation that just said it runs none.
    const sheet = answersFrom({
      draft: [
        {
          key: 'PROFILE_FACT_KEY_AI_SYSTEMS',
          value: { list: { values: [] } },
        },
      ],
    })
    expect(sheet.ai_systems).toEqual([])
    expect(
      ledger(sheet).find(
        (r) => r.obligation.slug === 'ai-act-art-50-transparency',
      )?.state,
    ).toBe('narrowed')
  })

  it('leaves a skipped question absent rather than guessing at it', () => {
    // A skip records no fact, so there is no draft entry, so the sheet has no
    // key. The corpus column then shows the obligation as still open, which is
    // the honest reading: nothing was recorded, so nothing was decided.
    const sheet = answersFrom({ draft: [] })
    expect(sheet.has_ropa).toBeUndefined()
    expect(
      ledger(sheet).find((r) => r.obligation.slug === 'gdpr-art-35-dpia')
        ?.state,
    ).toBe('pending')
  })

  it('reads an empty state as an empty sheet rather than throwing', () => {
    // What `GetOnboardingSession` answers for an organisation that has never
    // started: Connect omits a proto3 field at its zero value, so there is no
    // `draft` key at all.
    expect(answersFrom({})).toEqual({})
  })
})

describe('ledger', () => {
  it('opens with the obligations nothing could narrow, and the rest pending', () => {
    const rows = ledger(NOTHING)
    const counts = ledgerCounts(rows)
    expect(counts.narrowed).toBe(0)
    expect(counts.applies).toBeGreaterThan(0)
    expect(counts.pending).toBeGreaterThan(0)
    expect(counts.applies + counts.pending).toBe(OBLIGATIONS.length)
  })

  it('never reports an obligation as narrowed before its question is asked', () => {
    // The page's claim is that it did not guess, and showing an unanswered
    // obligation as decided either way breaks it.
    for (const row of ledger(NOTHING)) {
      expect(row.state).not.toBe('narrowed')
    }
  })

  it('closes an obligation the moment an answer rules it out', () => {
    const rows = ledger({ ...NOTHING, high_risk_processing: 'no' })
    const dpia = rows.find((r) => r.obligation.slug === 'gdpr-art-35-dpia')
    expect(dpia?.state).toBe('narrowed')
    expect(dpia?.reason).toContain('You said')
  })

  it('leaves nothing pending once every question is answered', () => {
    expect(ledgerCounts(ledger(typicalStartup())).pending).toBe(0)
  })

  it('agrees with assess on a finished answer sheet', () => {
    // Two code paths reading the same clauses. If they ever disagree, the
    // visitor watched one verdict resolve and was shown another.
    const answers = typicalStartup()
    const verdict = assess(answers)
    const applying = new Set(verdict.applies.map((a) => a.obligation.slug))
    for (const row of ledger(answers)) {
      expect(row.state === 'applies').toBe(applying.has(row.obligation.slug))
    }
  })
})
