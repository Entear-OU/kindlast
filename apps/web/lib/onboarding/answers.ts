/**
 * The answer sheet the console evaluates the corpus against (ENT-189, ENT-254).
 *
 * # THIS FILE USED TO HOLD THE QUESTIONS, AND DELIBERATELY NO LONGER DOES
 *
 * `/readiness` was a static page with no server, so its thirteen questions,
 * their options and their order all lived here, in the bundle. ENT-254 moved
 * the assessment inside `/o/{slug}/onboarding`, where every answer is parsed
 * into a typed fact by Go at the moment it is given, against a closed
 * vocabulary, with a refusal available.
 *
 * The questions therefore live in `apps/core-api/internal/domain/onboarding`
 * and arrive over the wire, options included. That is not tidiness. Two reasons
 * and both have a name attached:
 *
 *   - The transcript a customer reads back has to be the text they were
 *     actually asked (ENT-212). A prompt that lived only in a browser would
 *     leave `onboarding_messages` holding answers to questions nobody could
 *     reconstruct a year later.
 *   - A vocabulary declared in two languages is a vocabulary that drifts, and
 *     the drift is silent: a token the console invents produces a fact the
 *     applicability rules never match, which is an obligation quietly ceasing
 *     to apply to somebody. ENT-246 is a whole issue about that failure.
 *
 * What is left here is the shape of an answer sheet and the readers the
 * evaluator needs, plus the one label table the evaluator cannot get from the
 * question in front of it. Everything else came from the server.
 */
import type { DraftFact, OnboardingState } from './client'

/** The sentinel a person picks when they cannot answer a list question. */
export const UNSURE = 'unsure'

/** The option that means "none of these". */
export const NONE = 'none'

export type TriState = 'yes' | 'no' | 'unsure'

/** One answer: a tri-state, or the tokens picked from a closed list. */
export type Answer = TriState | readonly string[]

export type Answers = Readonly<Record<string, Answer>>

/**
 * The Article 6(1) bases, in the spelling `domain/memory` stores.
 *
 * The only label table left on this side, and it is here because of what reads
 * it: an obligation narrowed by `lawful_basis_includes` names a basis, and the
 * result has to say which of the person's own answers opened it, in words. That
 * sentence has to be renderable for an obligation whose question is not on
 * screen, so it cannot come from the question.
 *
 * The VALUES are the load-bearing half and they are asserted against the server
 * in `__tests__/lib/onboarding/evaluate.test.ts` by way of the corpus: every
 * `lawful_basis_includes` in `data/corpus/obligations.json` must appear here,
 * or the sentence explaining why Article 7 reached somebody would name a token.
 */
export const LAWFUL_BASIS_LABELS: Readonly<Record<string, string>> = {
  consent: 'The person agreed to it',
  contract: 'We need it to deliver what they asked for',
  legal_obligation: 'We are under a separate legal obligation to hold it',
  vital_interests: "Somebody's life could depend on it",
  public_task: 'We carry out a public task',
  legitimate_interests:
    'We have a business reason and weighed it against their interests',
}

/** How a lawful basis reads back in a sentence, or the token if it is unknown. */
export function lawfulBasisLabel(value: string): string {
  return LAWFUL_BASIS_LABELS[value] ?? value
}

/** The tokens a list answer holds, ignoring the two sentinels. */
export function named(answers: Answers, key: string): readonly string[] {
  const value = answers[key]
  if (!Array.isArray(value)) return []
  return value.filter((token) => token !== NONE && token !== UNSURE)
}

/** Everything a list answer holds, sentinels included. */
export function picked(answers: Answers, key: string): readonly string[] {
  const value = answers[key]
  return Array.isArray(value) ? value : []
}

export function tri(answers: Answers, key: string): TriState | undefined {
  const value = answers[key]
  return value === 'yes' || value === 'no' || value === 'unsure'
    ? value
    : undefined
}

/**
 * The answer sheet, read out of the interview's state.
 *
 * # WHY THE DRAFT AND NOT THE TRANSCRIPT
 *
 * `draft` is what the server took the answers to mean, typed, in script order,
 * with skips already absent. The transcript is what was said. Evaluating the
 * corpus against the second would mean parsing answers in the browser, which is
 * a second implementation of the rule that decides what a customer's profile
 * contains, and the day the two disagree the console shows an obligation the
 * product does not.
 *
 * A SKIPPED QUESTION IS ABSENT HERE, which is the same thing it is in the fact
 * store, so the corpus column shows it as still open rather than as decided.
 * That is the honest reading: nothing was recorded, so nothing was decided.
 */
export function answersFrom(state: OnboardingState): Answers {
  const sheet: Record<string, Answer> = {}
  for (const fact of state.draft ?? []) {
    if (!fact.key) continue
    const value = readDraft(fact)
    if (value !== undefined) sheet[storedKey(fact.key)] = value
  }
  return sheet
}

/**
 * The stored key behind a wire enum value.
 *
 * Derived rather than tabulated, because the two are the same string by
 * construction: `domain/memory` says the fact keys "are deliberately the same
 * names the legacy `compliance_profiles` columns used", and the proto enum was
 * written onto them. A table here would be a third place to keep in step, and
 * it would go stale silently, because a key it got wrong would simply never
 * match a rule in `evaluate.ts` and the obligation would quietly stop
 * resolving.
 *
 * `__tests__/lib/onboarding/evaluate.test.ts` drives the derivation for the
 * shapes that carry the longest names, which is where an off-by-one prefix
 * would show.
 */
function storedKey(wire: string): string {
  return wire.replace(/^PROFILE_FACT_KEY_/, '').toLowerCase()
}

function readDraft(fact: DraftFact): Answer | undefined {
  const value = fact.value
  if (!value) return undefined

  if (value.list) return value.list.values ?? []
  switch (value.triState) {
    case 'TRI_STATE_YES':
      return 'yes'
    case 'TRI_STATE_NO':
      return 'no'
    case 'TRI_STATE_UNSURE':
      return 'unsure'
    default:
      // Text and number facts exist in the vocabulary and the interview does
      // not collect them (ENT-254). Nothing in the corpus narrows on one, so
      // leaving them out of the sheet changes no result and keeps the sheet's
      // types honest about what a question can produce.
      return undefined
  }
}
