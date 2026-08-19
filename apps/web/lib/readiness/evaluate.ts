/**
 * Which obligations reach this visitor, and why (ENT-189).
 *
 * # THIS IS A PORT, AND FIDELITY MATTERS MORE THAN IMPROVEMENT
 *
 * `watcher_obligation_applies` and `watcher_gap_satisfied` (db/migrations/00001
 * and 00023) decide this in the product. This module decides it on a marketing
 * page for a visitor with no account, no organisation and no database row, and
 * the two have to agree, because a prospect who signs up after reading this and
 * sees a different answer has been told two things by the same company.
 *
 * So where a rule here looks wrong, it is deliberately the rule the database
 * runs, and the comment says so. The one worth noticing on review is the
 * asymmetry between the thresholds: `cross_border_transfers` needs a definite
 * yes (`transfers_outside_eu is distinct from 'yes'` narrows it away for
 * "unsure"), while `high_risk_processing`, `high_risk_ai_system` and
 * `large_scale_monitoring` go through `watcher_fact_affirms`, which counts
 * "unsure" as affirming. That is not a transcription error; it is two functions
 * written three migrations apart, and closing it belongs to whoever moves the
 * evaluator into Go under ENT-225, not to a marketing page.
 *
 * # THE TWO HALVES OF WHAT COMES OUT, AND WHY THEY ARE SEPARATE
 *
 * ENT-248's split, applied here:
 *
 *   - The statement of law is `obligation.summary`, verbatim from the corpus,
 *     and this module never touches it. It only decides which rows to hand to
 *     the renderer.
 *   - `because` and `gapNotes` are sentences about the visitor's own answers.
 *     They cite nothing, quantify over no class of legal person, and say
 *     nothing about what the law requires. `corpus.test.ts` runs every one of
 *     them past the same detector the code critic uses.
 *
 * # A GAP IS NOT A SELF-REPORTED WEAKNESS, AND THEY RENDER DIFFERENTLY
 *
 * `gaps` are the corpus's `requires` tokens the answers did not satisfy, which
 * is exactly what the Watcher would raise as a finding. `selfChecks` are the
 * visitor's own answers to questions the corpus attaches no token to (a written
 * DSAR process, a breach plan). Presenting the second as a Kindlast finding
 * would be asserting something the Watcher never said, which is the same
 * mistake as fabricating an obligation, one step smaller.
 */
import type { GapToken, Obligation } from './corpus'
import { OBLIGATIONS } from './corpus'
import {
  NONE,
  UNSURE,
  named,
  picked,
  optionLabel,
  questionFor,
  tri,
  type Answers,
  type TriState,
} from './script'

/**
 * One clause of an obligation's applicability, with the sentence to show
 * whichever way it went. Both sentences are written from the answers.
 */
interface Condition {
  readonly met: boolean
  /** The answer this clause reads, so a page can tell "no" from "not yet". */
  readonly reads: string
  /** Shown on the card when this obligation reached the visitor. */
  readonly whenMet: string
  /** Shown instead when this clause is what narrowed it away. */
  readonly whenUnmet: string
}

/** A question the visitor answered about their own readiness, carried through. */
export interface SelfCheck {
  readonly key: string
  readonly prompt: string
  readonly answer: TriState | undefined
}

export interface AppliedObligation {
  readonly obligation: Obligation
  /** Why it reached this visitor, said in terms of what they answered. */
  readonly because: readonly string[]
  /** Corpus `requires` tokens the answers did not satisfy. */
  readonly gaps: readonly GapToken[]
  /** One sentence per gap, again from the answers. */
  readonly gapNotes: readonly string[]
  /** The visitor's own words about their readiness. Never a Kindlast finding. */
  readonly selfChecks: readonly SelfCheck[]
}

export interface NarrowedObligation {
  readonly obligation: Obligation
  /** The answer that narrowed it away, so the visitor can see we did not guess. */
  readonly reason: string
}

export interface Assessment {
  readonly applies: readonly AppliedObligation[]
  readonly narrowed: readonly NarrowedObligation[]
  /** How many obligations the corpus holds, so the counts have a denominator. */
  readonly total: number
  /** Applying obligations with at least one corpus gap. */
  readonly withGaps: number
}

/**
 * The questions whose answers are the visitor's own readiness rather than an
 * input to applicability.
 *
 * `gdpr-art-34-breach-communication` shares `breach_plan` with Article 33 on
 * purpose: one plan answers both, and showing the visitor the same answer twice
 * beside two different corpus rows is more useful than picking one.
 */
const SELF_CHECKS: Readonly<Record<string, readonly string[]>> = {
  'gdpr-arts-12-22-data-subject-rights': ['dsar_process'],
  'gdpr-art-33-breach-notification': ['breach_plan'],
  'gdpr-art-34-breach-communication': ['breach_plan'],
}

/** yes or unsure. `watcher_fact_affirms`: absent is not affirmed, and neither is no. */
function affirms(answers: Answers, key: string): boolean {
  const value = tri(answers, key)
  return value === 'yes' || value === 'unsure'
}

/** How a tri-state answer reads back in a sentence about the visitor. */
function said(answers: Answers, key: string, claim: string): Condition {
  const value = tri(answers, key)
  return {
    met: affirms(answers, key),
    reads: key,
    whenMet:
      value === 'unsure'
        ? `You said you do not know whether ${claim}.`
        : `You said ${claim}.`,
    whenUnmet:
      value === 'no'
        ? `You said it is not the case that ${claim}.`
        : `Nobody asked you whether ${claim}, so there are no grounds to raise it.`,
  }
}

/**
 * Every applicability clause an obligation declares, in the order the database
 * evaluates them, so the first unmet one is the reason the database would give.
 */
function conditions(obligation: Obligation, answers: Answers): Condition[] {
  const when = obligation.appliesWhen
  const out: Condition[] = []
  if (!when) return out

  // role. Only deployer and provider narrow anything, and they narrow on an AI
  // system being in use rather than on a declared role, which is why the script
  // never asks the visitor which one they are.
  if (when.role === 'deployer' || when.role === 'provider') {
    const ai = picked(answers, 'ai_systems')
    const inUse = ai.length > 0 && !ai.every((token) => token === NONE)
    out.push({
      met: inUse,
      reads: 'ai_systems',
      whenMet: ai.includes(UNSURE)
        ? 'You said you could not say what AI is in use, which is not the same as none.'
        : 'You said AI is in use.',
      whenUnmet:
        ai.length === 0
          ? 'Nobody asked you what AI is in use.'
          : 'You said no AI is in use.',
    })
  }

  const thresholds = when.thresholds ?? {}

  if (thresholds.cross_border_transfers) {
    // A DEFINITE YES, and unsure is not one. See the header: this is the
    // database's asymmetry, kept rather than tidied.
    const value = tri(answers, 'transfers_outside_eu')
    out.push({
      met: value === 'yes',
      reads: 'transfers_outside_eu',
      whenMet: 'You said personal information leaves the EU or the EEA.',
      whenUnmet:
        value === 'unsure'
          ? 'You said you do not know whether personal information leaves the EU or the EEA, and Kindlast opens this one only on a definite yes.'
          : value === 'no'
            ? 'You said nothing leaves the EU or the EEA.'
            : 'Nobody asked you whether anything leaves the EU or the EEA.',
    })
  }

  if (thresholds.high_risk_processing) {
    out.push(
      said(
        answers,
        'high_risk_processing',
        'what you do with personal information creates a serious risk to the people it is about',
      ),
    )
  }

  if (thresholds.high_risk_ai_system) {
    out.push(
      said(
        answers,
        'high_risk_ai_system',
        "some of your AI falls inside the EU AI Act's high-risk list",
      ),
    )
  }

  if (thresholds.large_scale_monitoring) {
    out.push(
      said(
        answers,
        'large_scale_monitoring',
        'you track or monitor people regularly, systematically, and at scale',
      ),
    )
  }

  if (when.lawful_basis_includes) {
    const bases = named(answers, 'lawful_bases')
    const basis = when.lawful_basis_includes
    const label = optionLabel('lawful_bases', basis)
    out.push({
      met: bases.includes(basis),
      reads: 'lawful_bases',
      whenMet: `You said one of your grounds is that ${lowerFirst(label)}.`,
      whenUnmet: `You did not name "${label}" among the grounds you rely on.`,
    })
  }

  if (when.engages_processor) {
    const vendors = picked(answers, 'vendor_list')
    const engages = vendors.length > 0 && !vendors.every((t) => t === NONE)
    out.push({
      met: engages,
      reads: 'vendor_list',
      whenMet: vendors.includes(UNSURE)
        ? 'You said you could not say who handles personal information for you, which is not the same as nobody.'
        : 'You named other companies that handle personal information for you.',
      whenUnmet:
        vendors.length === 0
          ? 'Nobody asked you who handles personal information for you.'
          : 'You said nobody outside the company touches it.',
    })
  }

  return out
}

function lowerFirst(text: string): string {
  return text.charAt(0).toLowerCase() + text.slice(1)
}

/** Faithful port of `watcher_obligation_applies`. */
export function obligationApplies(
  obligation: Obligation,
  answers: Answers,
): boolean {
  return conditions(obligation, answers).every((c) => c.met)
}

/**
 * Faithful port of `watcher_gap_satisfied`.
 *
 * An unknown token returns true, exactly as the plpgsql does, and for the same
 * uncomfortable reason: an unrecognised rule must not raise a gap. The reason
 * it is safe here and dangerous there is that the corpus's tokens are a closed
 * TypeScript union, so an unknown one is a compile error rather than a silent
 * runtime shrug.
 */
export function gapSatisfied(token: GapToken, answers: Answers): boolean {
  switch (token) {
    case 'ropa':
      return tri(answers, 'has_ropa') === 'yes'
    case 'dpo':
      return tri(answers, 'has_dpo') === 'yes'
    case 'ai_register': {
      // Satisfied only when no AI system is operated. Using AI with nothing
      // written down is the gap, and there is no register field to read.
      const ai = picked(answers, 'ai_systems')
      return ai.length === 0 || ai.every((t) => t === NONE)
    }
    case 'transfer_safeguards': {
      // Satisfied when at least one destination is documented. "I could not
      // say" documents nothing, which is why `named` drops the sentinel.
      return named(answers, 'transfer_destinations').length > 0
    }
    default:
      return true
  }
}

/** The sentence shown against a gap, written from the answer that produced it. */
function gapNote(token: GapToken, answers: Answers): string {
  switch (token) {
    case 'ropa':
      return tri(answers, 'has_ropa') === 'unsure'
        ? 'You said you do not know whether a record of processing activities is kept.'
        : 'You said no record of processing activities is kept.'
    case 'dpo':
      return tri(answers, 'has_dpo') === 'unsure'
        ? 'You said you do not know whether a data protection officer has been appointed.'
        : 'You said no data protection officer has been appointed.'
    case 'ai_register':
      // NOT "Kindlast has nothing written down about which systems those are".
      // That sentence implied this page holds a record about the reader, on a
      // surface whose whole claim is that it writes nothing down, and it put
      // the gap on us rather than on them. Its two siblings above are about the
      // visitor, and so is this one now.
      return 'You said AI is in use, and nothing you told us shows those systems are written down.'
    case 'transfer_safeguards':
      return 'You did not name anywhere the information goes.'
    default:
      return ''
  }
}

/** The self-reported answers to carry onto an obligation's card. */
function selfChecksFor(slug: string, answers: Answers): SelfCheck[] {
  return (SELF_CHECKS[slug] ?? []).map((key) => ({
    key,
    prompt: questionFor(key)?.prompt ?? key,
    answer: tri(answers, key),
  }))
}

/**
 * Run the whole corpus against one answer sheet.
 *
 * Pure, synchronous, and it touches nothing outside its arguments. That is the
 * property the no-persistence guarantee rests on: there is no client to mock
 * out, no fetch to intercept and no store to forget to clear, because the
 * assessment is arithmetic.
 */
export function assess(answers: Answers): Assessment {
  const applies: AppliedObligation[] = []
  const narrowed: NarrowedObligation[] = []

  for (const obligation of OBLIGATIONS) {
    const clauses = conditions(obligation, answers)
    const firstUnmet = clauses.find((c) => !c.met)

    if (firstUnmet) {
      narrowed.push({ obligation, reason: firstUnmet.whenUnmet })
      continue
    }

    const because =
      clauses.length > 0
        ? clauses.map((c) => c.whenMet)
        : // Nothing in the corpus narrows this one, so nothing the visitor said
          // could have. Said plainly rather than left blank: an empty
          // explanation reads as a claim we could not support.
          ['Nothing you told us narrows this one.']

    const gaps = (obligation.appliesWhen?.requires ?? []).filter(
      (token) => !gapSatisfied(token, answers),
    )

    applies.push({
      obligation,
      because,
      gaps,
      gapNotes: gaps.map((token) => gapNote(token, answers)),
      selfChecks: selfChecksFor(obligation.slug, answers),
    })
  }

  return {
    applies,
    narrowed,
    total: OBLIGATIONS.length,
    withGaps: applies.filter((a) => a.gaps.length > 0).length,
  }
}

/**
 * Where every obligation stands part way through, for the live corpus column.
 *
 * # WHY "PENDING" IS A THIRD STATE AND NOT A ROUNDING
 *
 * `assess` has two outcomes because a finished answer sheet has two: an
 * obligation reached you or an answer narrowed it away. Half way through the
 * interview there is a third, and collapsing it into either one is a lie the
 * visitor can catch. Showing an unanswered obligation as narrowed says we
 * decided; showing it as applying says we decided the other way. Neither is
 * true before the question is asked, and the page's whole claim is that we did
 * not guess.
 *
 * So a clause that is unmet because its answer has not been given yet leaves
 * the obligation open, and a clause that is unmet because of an answer that WAS
 * given closes it. Where both are true the answer wins, because something the
 * visitor actually said already rules it out and re-opening it would be
 * theatre.
 */
export type LedgerState = 'applies' | 'narrowed' | 'pending'

export interface LedgerRow {
  readonly obligation: Obligation
  readonly state: LedgerState
  /** Present for `narrowed`: the answer that closed it. */
  readonly reason?: string
}

export function ledger(answers: Answers): readonly LedgerRow[] {
  return OBLIGATIONS.map((obligation) => {
    const clauses = conditions(obligation, answers)
    const closed = clauses.find((c) => !c.met && answers[c.reads] !== undefined)
    if (closed) {
      return {
        obligation,
        state: 'narrowed' as const,
        reason: closed.whenUnmet,
      }
    }
    if (clauses.some((c) => !c.met)) {
      return { obligation, state: 'pending' as const }
    }
    return { obligation, state: 'applies' as const }
  })
}

export function ledgerCounts(
  rows: readonly LedgerRow[],
): Record<LedgerState, number> {
  return {
    applies: rows.filter((r) => r.state === 'applies').length,
    narrowed: rows.filter((r) => r.state === 'narrowed').length,
    pending: rows.filter((r) => r.state === 'pending').length,
  }
}
