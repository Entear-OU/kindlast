/**
 * The readiness page's own prose (ENT-189).
 *
 * # WHY MARKETING COPY IS IN A MODULE AND NOT IN THE JSX
 *
 * So it can be tested. Every string here is run past the legal-claim detector
 * in `claims.ts` by `__tests__/lib/readiness/copy.test.ts`, for the same reason
 * `apps/intelligence` runs a model's narrative past the Python original: the
 * sentence is what matters, not who wrote it.
 *
 * A landing page is where that guard is weakest by default. Product copy is
 * written to persuade, the person writing it is usually not the person who read
 * Article 30(5), and the reader has no compliance record on screen to check it
 * against. Leaving these sentences inline in a component would mean the only
 * thing standing between a confident summary of the law and a stranger is
 * whoever reviews the pull request.
 *
 * The strings that are NOT here are the ones that carry no risk: a button
 * label, a step counter, an aria description. Those stay next to the element
 * they belong to, because moving them here would say they had been checked for
 * something they cannot be wrong about.
 */

/** The one-line promise, and the two facts that make it true. */
export const HERO_EYEBROW = 'No account · No request · Nothing recorded'

export const HERO_LEAD = 'Find out what applies'
export const HERO_LEAD_ACCENT = 'before somebody else tells you.'

export const HERO_SUB =
  'Answer the questions a data protection officer would ask. Kindlast matches ' +
  'them against the obligations it holds, quotes the source behind every one, ' +
  'and shows you which answer of yours put it there.'

/** Said on the interview and again on the result, because it is the whole deal. */
export const NO_TRANSMISSION =
  'Your answers never leave this page. It makes no request, sets no cookie, ' +
  'and writes nothing down: close the tab and the assessment is gone with it.'

/** The section headings on the result, which are the design's load-bearing idea. */
export const HEADING_LAW = 'What Kindlast holds on this'
export const HEADING_WHY = 'Why it reached you'
export const HEADING_GAP = 'What you told us is missing'
export const HEADING_SELF = 'What you told us about your own readiness'
export const HEADING_SET_ASIDE = 'Set aside, and why'

/**
 * Under `HEADING_LAW`, every time. The point of the whole surface is that this
 * paragraph was not written for the web.
 */
export const QUOTE_PROVENANCE =
  'Quoted from the Kindlast corpus, unedited. Follow the citation to read the ' +
  'official text.'

/** Under `HEADING_WHY`. Separates our matcher from a claim about the law. */
export const WHY_PROVENANCE =
  'Written from your answers alone. Nothing in this part is a statement about ' +
  'what you owe.'

export const SET_ASIDE_LEAD =
  'These did not reach you. The answer that ruled each one out is beside it, ' +
  'because a result you cannot check is worth nothing.'

/**
 * The disclaimer, and it is deliberately not in small print at the bottom.
 *
 * "Not an audit" is one of the three things ENT-189 puts out of scope, and the
 * honest way to keep it out of scope is to say so where the result is, not to
 * hope the footer covers it.
 */
export const NOT_AN_AUDIT =
  'This is not an audit, and it is not legal advice. It is the Kindlast corpus ' +
  'matched against what you said, by the same rules the product runs on a ' +
  'customer, and every line shows you which answer put it there.'

export const REACHED_YOU_MEANS =
  'Reached you means the matcher opened it on your answers. It is not a ' +
  'finding, and nobody has looked at your company.'

/** Shown where a visitor might expect an email capture, because there is none. */
export const NO_EMAIL_ASKED =
  'There is no email box at the end. Sending you this would mean sending your ' +
  'answers to a mail provider and keeping your address, and neither is worth ' +
  'doing before we have written down what we would do with them.'

export const GAP_MEANS =
  'A gap is a control you told us is not in place. It is what the product ' +
  'would raise once it was watching, not something anyone has verified.'

export const SELF_MEANS =
  'Your own answer, carried through as you gave it. Kindlast raises nothing ' +
  'against these, and repeating them here is so the picture is complete.'
