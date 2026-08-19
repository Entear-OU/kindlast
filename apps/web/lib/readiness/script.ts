/**
 * The readiness assessment's question set (ENT-189).
 *
 * # A SCRIPT, FOR THE REASON ENT-212 GAVE, AND ONE MORE
 *
 * The onboarding interview is a fixed script with typed answers and no model
 * call, because the profile decides which obligations apply and a field a model
 * invented produces wrong findings later, at enough distance from the mistake
 * that nobody traces it back. Everything in that argument holds here.
 *
 * The extra reason is the one the issue raised as risk 1: an unauthenticated
 * LLM endpoint is a cost and abuse vector, and it wanted per-IP rate limiting,
 * a bot check, a turn cap and a spend alert before shipping. A script needs
 * none of those, because there is no endpoint. Every question below is
 * answered by tapping a button, every answer is one of a closed set of tokens
 * this file declares, and the evaluation is a pure function. Nothing a visitor
 * does costs a model call, so nothing has to be rationed.
 *
 * It also means nothing a visitor types can be concatenated into a prompt,
 * because there is nothing to type and no prompt. AGENTS.md's rule that user
 * input is data rather than instruction is satisfied by there being no
 * instruction channel at all.
 *
 * # THE KEYS ARE THE PRODUCT'S FACT KEYS, NOT NEW ONES
 *
 * `has_ropa`, `transfers_outside_eu` and the rest are spelled as
 * `apps/core-api/internal/domain/memory` spells them, and the lawful bases are
 * the Article 6(1) closed set in that package's spelling. Nothing here writes a
 * fact, but a second iteration that offers to carry the answers into an account
 * should not have to translate them, and a divergent spelling is how Article 7
 * silently stops applying to anybody.
 *
 * Two keys are local to this surface and are marked as such below:
 * `dsar_process` and `breach_plan`. They ask what a DPO would ask, the corpus
 * raises no gap token for either, and they are rendered as the visitor's own
 * words about their own readiness rather than as anything Kindlast found.
 *
 * # NO QUESTION STATES WHAT THE LAW REQUIRES
 *
 * ENT-248's ruling, and `__tests__/lib/readiness/corpus.test.ts` enforces it by
 * running every prompt, every help string and every option label past the same
 * detector the code critic uses. Where a visitor reasonably wants to know why
 * they are being asked, the question carries a `basis` slug and the UI renders
 * that obligation's corpus summary, verbatim. The law is quoted, never
 * paraphrased for the web.
 */

/** The sentinel a visitor picks when they cannot answer a list question. */
export const UNSURE = 'unsure'

/** The option that means "none of these", and clears everything else. */
export const NONE = 'none'

export type TriState = 'yes' | 'no' | 'unsure'

/** One answer: a tri-state, or the tokens picked from a closed list. */
export type Answer = TriState | readonly string[]

export type Answers = Readonly<Record<string, Answer>>

export interface Option {
  readonly value: string
  readonly label: string
  /**
   * Picking this clears every other choice. `none` and `unsure` are both
   * exclusive: "nobody outside the company touches it" and "I could not say"
   * are each a complete answer, and neither combines with naming a supplier.
   */
  readonly exclusive?: boolean
}

interface BaseQuestion {
  readonly key: string
  readonly prompt: string
  readonly help?: string
  /**
   * The corpus obligation to quote when the visitor asks why we want to know.
   * The text shown is that obligation's `summary`, unedited.
   */
  readonly basis?: string
}

export interface TriQuestion extends BaseQuestion {
  readonly kind: 'tri'
}

export interface MultiQuestion extends BaseQuestion {
  readonly kind: 'multi'
  readonly options: readonly Option[]
}

export type Question = TriQuestion | MultiQuestion

/**
 * The interview, in order.
 *
 * It is the DPO question set ENT-189 named (processing activities, lawful
 * basis, categories of personal data, third-country transfers, processors and
 * sub-processors, DPO status, breach process, DSAR process, AI systems and
 * their risk tier), narrowed to the questions whose answers change something.
 *
 * WHAT IS DELIBERATELY NOT ASKED, and why, because the absences look like
 * oversights:
 *
 *   - Controller or processor. `watcher_obligation_applies` imposes no
 *     restriction for `role: controller`, and gates `deployer` and `provider`
 *     on an AI system being in use rather than on a declared role. Asking would
 *     produce an answer nothing reads, and an answer nothing reads is how a
 *     visitor concludes the result depends on something it does not.
 *   - Whether an AI register is kept. The `ai_register` gap token is satisfied
 *     only when no AI system is operated, so the answer could not change the
 *     result either way.
 *   - Headcount. `employees_min` was deleted from the vocabulary in ENT-246
 *     precisely so nobody encodes Article 30(5) as a number, and asking for a
 *     figure this surface then visibly ignores invites the reader to encode it
 *     themselves.
 */
export const SCRIPT: readonly Question[] = [
  {
    key: 'data_categories',
    kind: 'multi',
    prompt: 'What kinds of personal information does your company hold?',
    help: 'Pick everything that applies. If you are unsure whether something counts, pick it.',
    options: [
      { value: 'contact_details', label: 'Names and contact details' },
      { value: 'account', label: 'Account, device or online identifiers' },
      { value: 'payment', label: 'Payment or financial details' },
      { value: 'health', label: 'Health or medical information' },
      { value: 'biometric', label: 'Biometric or genetic information' },
      { value: 'location', label: 'Location or movement data' },
      { value: 'employment', label: 'Employment and HR records' },
      { value: 'behaviour', label: 'Behaviour, usage or tracking data' },
      { value: 'children', label: 'Information about children' },
      { value: NONE, label: 'None of this', exclusive: true },
    ],
  },
  {
    key: 'lawful_bases',
    kind: 'multi',
    prompt: 'On what grounds do you use it?',
    help: 'Pick every one you rely on. Most organisations rely on more than one.',
    basis: 'gdpr-art-6-lawful-basis',
    options: [
      // The values are the Article 6(1) closed set as `domain/memory` spells
      // it. The labels are the plain-English version; the values are what a
      // second iteration would store and what the corpus is matched against.
      { value: 'consent', label: 'The person agreed to it' },
      { value: 'contract', label: 'We need it to deliver what they asked for' },
      {
        value: 'legal_obligation',
        label: 'We are under a separate legal obligation to hold it',
      },
      { value: 'vital_interests', label: "Somebody's life could depend on it" },
      { value: 'public_task', label: 'We carry out a public task' },
      {
        value: 'legitimate_interests',
        label:
          'We have a business reason and weighed it against their interests',
      },
      { value: UNSURE, label: 'I could not say', exclusive: true },
    ],
  },
  {
    key: 'vendor_list',
    kind: 'multi',
    prompt: 'Which other companies handle that information for you?',
    help: 'The suppliers who touch personal information on your behalf, and the ones they use in turn.',
    basis: 'gdpr-art-28-processor-contracts',
    options: [
      { value: 'hosting', label: 'Hosting or cloud infrastructure' },
      { value: 'payments', label: 'Payments' },
      { value: 'email', label: 'Email and marketing' },
      { value: 'analytics', label: 'Analytics and product tracking' },
      { value: 'support', label: 'Support desk or CRM' },
      { value: 'hr', label: 'HR and payroll' },
      { value: 'ai_vendors', label: 'AI or model providers' },
      {
        value: NONE,
        label: 'Nobody outside the company touches it',
        exclusive: true,
      },
      // Not folded into "nobody". Not knowing who handles your data is a
      // different situation from nobody handling it, and only the second one
      // narrows the obligation away.
      { value: UNSURE, label: 'I could not say', exclusive: true },
    ],
  },
  {
    key: 'transfers_outside_eu',
    kind: 'tri',
    prompt: 'Does any of that information leave the EU or the EEA?',
    help: 'Anything hosted, backed up, or supported from outside the EU or EEA is inside this question.',
    basis: 'gdpr-chapter-v-international-transfers',
  },
  {
    key: 'transfer_destinations',
    kind: 'multi',
    prompt: 'Where does it go?',
    help: 'Name the places you know about.',
    options: [
      { value: 'united_states', label: 'United States' },
      { value: 'united_kingdom', label: 'United Kingdom' },
      { value: 'canada', label: 'Canada' },
      { value: 'india', label: 'India' },
      { value: 'australia', label: 'Australia' },
      { value: 'japan', label: 'Japan' },
      { value: 'elsewhere', label: 'Somewhere else' },
      { value: UNSURE, label: 'I could not say', exclusive: true },
    ],
  },
  {
    key: 'high_risk_processing',
    kind: 'tri',
    prompt:
      'Does what you do with that information create a serious risk to the people it is about?',
    help: 'Answer for how it looks from where you sit. "Not sure" is a real answer here and is treated as one.',
    basis: 'gdpr-art-35-dpia',
  },
  {
    key: 'large_scale_monitoring',
    kind: 'tri',
    prompt:
      'Do you track or monitor people regularly, systematically, and at scale?',
    help: 'Continuous behavioural tracking, location histories, or cameras covering places the public can walk into.',
    basis: 'gdpr-art-37-dpo-appointment',
  },
  {
    key: 'has_ropa',
    kind: 'tri',
    prompt: 'Do you keep a record of processing activities?',
    help: 'A written list of what you do with personal information and why. It is often a spreadsheet.',
    basis: 'gdpr-art-30-ropa',
  },
  {
    key: 'has_dpo',
    kind: 'tri',
    prompt: 'Have you appointed a data protection officer?',
    help: 'A named person formally appointed to be responsible for data protection.',
    basis: 'gdpr-art-37-dpo-appointment',
  },
  {
    key: 'dsar_process',
    kind: 'tri',
    // Local to this surface: the corpus raises no gap token for Articles 12 to
    // 22, so this answer is reported back as the visitor's own words and never
    // as something Kindlast found.
    prompt:
      'If somebody asked today for a copy of everything you hold on them, is there a written process for answering?',
    help: 'A process somebody other than you could follow while you were on holiday.',
    basis: 'gdpr-arts-12-22-data-subject-rights',
  },
  {
    key: 'breach_plan',
    kind: 'tri',
    // Local to this surface, same as above.
    prompt:
      'Is there a written plan for the hours after personal information is lost, leaked or exposed?',
    help: 'Who gets called, who decides, and who writes down what happened.',
    basis: 'gdpr-art-33-breach-notification',
  },
  {
    key: 'ai_systems',
    kind: 'multi',
    prompt: 'Which AI is in use, inside your product or by your team?',
    help: 'Include the ones that feel too ordinary to mention.',
    basis: 'ai-act-art-4-ai-literacy',
    options: [
      { value: 'assistants', label: 'Bought-in assistants and copilots' },
      { value: 'in_product', label: 'AI features inside our own product' },
      { value: 'built', label: 'Models we build or fine-tune ourselves' },
      {
        value: 'embedded',
        label: 'AI inside tools we bought for something else',
      },
      { value: NONE, label: 'None that we know of', exclusive: true },
      { value: UNSURE, label: 'I could not say', exclusive: true },
    ],
  },
  {
    key: 'high_risk_ai_system',
    kind: 'tri',
    // The question names the classification rather than describing it, and the
    // description comes from the Annex III corpus row the UI renders beside it.
    // Summarising Annex III here in our own words is exactly the sentence
    // ENT-248 says belongs to the corpus.
    prompt: "Does any of that AI fall inside the EU AI Act's high-risk list?",
    help: 'The list is quoted below, straight from what Kindlast holds. Answer "not sure" if nobody has checked.',
    basis: 'ai-act-annex-iii-high-risk-systems',
  },
]

/** An answer sheet with nothing on it. */
export function emptyAnswers(): Answers {
  return {}
}

export function questionFor(key: string): Question | undefined {
  return SCRIPT.find((q) => q.key === key)
}

/** The options a list question offers, or nothing if it is not a list question. */
export function optionsFor(key: string): readonly Option[] {
  const question = questionFor(key)
  return question?.kind === 'multi' ? question.options : []
}

/** How an option reads back in a sentence, or the token if nothing offers it. */
export function optionLabel(key: string, value: string): string {
  return optionsFor(key).find((o) => o.value === value)?.label ?? value
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
 * Whether a question is worth asking, given what has been answered so far.
 *
 * Two rules, both lifted from `onboarding.Applicable`'s reasoning: a question
 * with no meaning for this visitor should not be asked, because the answer is
 * either an awkward "not applicable" or a skip that reads as a refusal.
 *
 * ONLY A DEFINITE NO REMOVES A QUESTION. "We do not know whether anything
 * leaves the EU" is not the same claim as "nothing does", and an organisation
 * that cannot say what AI it runs still has an answer worth having about
 * whether any of it decides about people.
 */
export function applicable(key: string, answers: Answers): boolean {
  if (key === 'transfer_destinations') {
    return tri(answers, 'transfers_outside_eu') !== 'no'
  }
  if (key === 'high_risk_ai_system') {
    const ai = picked(answers, 'ai_systems')
    return ai.length === 0 || !ai.every((token) => token === NONE)
  }
  return true
}

/** The questions this visitor will be asked, in order. */
export function applicableQuestions(answers: Answers): readonly Question[] {
  return SCRIPT.filter((q) => applicable(q.key, answers))
}

/** What to ask next, or nothing when the interview is done. */
export function nextQuestion(answers: Answers): Question | undefined {
  return applicableQuestions(answers).find((q) => answers[q.key] === undefined)
}

/** How far through, for a progress indicator that does not lie when a branch closes. */
export function progress(answers: Answers): {
  answered: number
  total: number
} {
  const questions = applicableQuestions(answers)
  return {
    answered: questions.filter((q) => answers[q.key] !== undefined).length,
    total: questions.length,
  }
}
