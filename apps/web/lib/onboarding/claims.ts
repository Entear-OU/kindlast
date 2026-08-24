/**
 * The claim detector, on the marketing site (ENT-189, holding ENT-248).
 *
 * # WHY THE SAME PATTERNS EXIST TWICE
 *
 * `apps/intelligence/.../harness/claims.py` refuses a model's free text when it
 * asserts law. This file is that detector in TypeScript, and it guards a
 * surface with no model on it at all.
 *
 * That sounds redundant and is not. ENT-248's ruling is about the SENTENCE, not
 * about who wrote it: the statement of law comes from the corpus row, and
 * anything written beside it explains applicability to this organisation. A
 * human writing marketing copy can break that rule exactly as a 2B model did,
 * and the failure is worse here. The observed narrative cited Article 30
 * correctly and stated the opposite of Article 30(5) beside it; a reader of an
 * authenticated finding has the corpus row on the same screen to check it
 * against, and a reader of a landing page has nothing.
 *
 * So every string this surface writes for itself (question prompts, help text,
 * option labels, the "why this reached you" sentences) is run past this at test
 * time, and the corpus summaries are rendered verbatim beside them.
 *
 * # WHAT IT CATCHES AND WHAT IT DOES NOT
 *
 * The shape of a legal assertion: a provision reference, a universal quantifier
 * over legal subjects, "regardless of", the vocabulary of exemptions and
 * thresholds, an instrument as the subject of a requirement verb, the passive
 * form of the same, and a class of legal persons as the subject of "must".
 *
 * Second person is deliberately allowed, and the reasoning transfers exactly:
 * "you" is this organisation, which is what the surface is entitled to talk
 * about. "Controllers must keep a record" is the corpus's sentence to make.
 *
 * It is lexical, not a reasoning check, so a false statement of law that avoids
 * every shape gets through. That gap is real and it is the reason the
 * structural half matters more: on this page nothing writes a statement of law
 * in the first place, so there is nothing for the detector to miss.
 *
 * # THIS IS A TEST-TIME GUARD, NOT A RUNTIME FILTER
 *
 * Nothing on this surface is generated, so there is no output to intercept.
 * Running it at build time over static copy is the whole control, and running
 * it at request time would be a filter over strings that cannot change.
 */

/** The subjects a claim about the law is made about. Second person is absent. */
const LEGAL_SUBJECT =
  'controllers?|processors?|organisations?|organizations?|companies|company' +
  '|businesses|business|firms?|deployers?|providers?|entities|entity'

/** The instruments. An assertion whose subject is a regulation states law. */
const INSTRUMENT =
  'the law|the regulation|the gdpr|gdpr|the ai act|the act|article \\d+'

export interface LegalAssertionPattern {
  /** What the record shows when this one fires. */
  readonly name: string
  readonly pattern: RegExp
}

/**
 * Mirrors `PATTERNS` in `claims.py`, in the same order and with the same names.
 * Kept as a list rather than one alternation so a failure names the rule that
 * objected rather than the whole regex.
 */
export const LEGAL_ASSERTION_PATTERNS: readonly LegalAssertionPattern[] = [
  {
    name: 'a provision reference',
    pattern: /\b(?:articles?|arts?\.|recitals?|annexe?s?)\s*\d+/i,
  },
  {
    name: 'a provision reference',
    pattern: /\bannexe?s?\s+[ivxlc]+\b/i,
  },
  {
    name: 'a claim about who the law applies to',
    pattern: new RegExp(
      '\\bapplies\\s+to\\s+(?:every|all|any|each|both)\\b' +
        '|\\bapply\\s+to\\s+(?:every|all|any|each|both)\\b' +
        '|\\b(?:every|all|any|each)\\s+(?:' +
        LEGAL_SUBJECT +
        ')\\b',
      'i',
    ),
  },
  {
    name: 'a claim that the law admits no exception',
    pattern:
      /\bregardless\s+of\b|\birrespective\s+of\b|\bno\s+matter\s+how\b|\bwithout\s+exception\b|\bin\s+all\s+cases\b/i,
  },
  {
    name: 'a claim about an exemption or a threshold',
    pattern:
      /\bexempt\w*\b|\bexception\w*\b|\bderogation\w*\b|\bcarve[- ]?out\b|\bthresholds?\b|\bfewer\s+than\s+\d+\b|\bmore\s+than\s+\d+\s+employees\b|\bunder\s+\d+\s+employees\b|\bat\s+least\s+\d+\s+employees\b/i,
  },
  {
    name: 'a statement of what the law requires',
    pattern: new RegExp(
      '\\b(?:' +
        INSTRUMENT +
        ')\\s+(?:requires?|mandates?|obliges?|demands?|states?|says?|provides?' +
        '|stipulates?|prohibits?|forbids?|permits?|allows?)\\b',
      'i',
    ),
  },
  {
    // The passive, which the first version of the Python file missed and a live
    // run produced: "the written records required by the law".
    name: 'a statement of what the law requires',
    pattern: new RegExp(
      '\\b(?:required|mandated|obliged|obligated|prohibited|permitted' +
        '|forbidden|exempted|demanded)\\s+(?:by|under)\\s+(?:law\\b|' +
        INSTRUMENT +
        ')',
      'i',
    ),
  },
  {
    name: 'an obligation stated over a class of organisations',
    pattern: new RegExp(
      '\\b(?:' +
        LEGAL_SUBJECT +
        ')\\s+(?:must|shall|are\\s+required\\s+to|is\\s+required\\s+to' +
        '|have\\s+to|has\\s+to|are\\s+obliged\\s+to|need\\s+to)\\b',
      'i',
    ),
  },
]

export interface LegalAssertion {
  readonly rule: string
  readonly matched: string
}

/** Every rule that objects to a string, so one rewrite can fix all of them. */
export function legalAssertions(text: string): LegalAssertion[] {
  const found: LegalAssertion[] = []
  for (const { name, pattern } of LEGAL_ASSERTION_PATTERNS) {
    const match = pattern.exec(text)
    if (match) found.push({ rule: name, matched: match[0] })
  }
  return found
}

/** Whether a string asserts law and therefore belongs to the corpus, not to us. */
export function assertsLaw(text: string): boolean {
  return legalAssertions(text).length > 0
}
