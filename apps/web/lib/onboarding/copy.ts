/**
 * Onboarding's own prose (ENT-189, ENT-254).
 *
 * # WHY THIS COPY IS IN A MODULE AND NOT IN THE JSX
 *
 * So it can be tested. Every string here is run past the legal-claim detector
 * in `claims.ts` by `__tests__/lib/onboarding/copy.test.ts`, for the same
 * reason `apps/intelligence` runs a model's narrative past the Python original:
 * the sentence is what matters, not who wrote it.
 *
 * The guard is weakest by default exactly where copy is written to reassure.
 * The person writing it is usually not the person who read Article 30(5), and
 * a sentence that summarises the law in its own words beside a correct citation
 * is the failure ENT-248 exists to stop, because no citation validator can see
 * it. Leaving these sentences inline in a component would mean the only thing
 * standing between a confident summary of the law and a customer is whoever
 * reviews the pull request.
 *
 * The strings that are NOT here are the ones that carry no risk: a button
 * label, a step counter, an aria description. Those stay next to the element
 * they belong to, because moving them here would say they had been checked for
 * something they cannot be wrong about.
 *
 * # WHAT WENT WHEN THE PUBLIC PAGE WENT
 *
 * The hero strings and `NO_TRANSMISSION` were `/readiness`'s, and they were
 * true of it: a static page that made no request and wrote nothing down. None
 * of that is true here, and carrying the sentences across would have been the
 * worst kind of stale copy, a promise about data handling that the surface
 * stopped keeping. What replaces them says what this surface actually does,
 * which is write every answer down.
 */

/** Said while the interview is running, because it is the deal now. */
export const ANSWERS_ARE_SAVED =
  'Your answers are saved as you give them, so you can stop and come back. ' +
  'Every one of them is yours to correct afterwards, and correcting one keeps ' +
  'what it said before.'

/** The section headings on the result, which are the design's load-bearing idea. */
export const HEADING_LAW = 'What Kindlast holds on this'
export const HEADING_WHY = 'Why it reached you'
export const HEADING_GAP = 'What you told us is missing'
export const HEADING_SET_ASIDE = 'Set aside, and why'

/**
 * Under `HEADING_LAW`, every time. The point of the whole surface is that this
 * paragraph was not written for a screen.
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
 * "Not an audit" was one of the three things ENT-189 put out of scope, and the
 * honest way to keep it out of scope is to say so where the result is, not to
 * hope a footer covers it. That holds here for a stronger reason: a customer
 * who has just signed up has more reason to read this as a verdict than a
 * passing visitor did.
 */
export const NOT_AN_AUDIT =
  'This is not an audit, and it is not legal advice. It is the Kindlast corpus ' +
  'matched against what you said, by the same rules the product runs from now ' +
  'on, and every line shows you which answer put it there.'

export const REACHED_YOU_MEANS =
  'Reached you means the matcher opened it on your answers. It is not a ' +
  'finding, and nobody has looked at your company yet.'

export const GAP_MEANS =
  'A gap is a control you told us is not in place. It is what the product ' +
  'will raise now that it is watching, not something anyone has verified.'

/** Where the result hands over to the rest of the product. */
export const WHAT_HAPPENS_NEXT =
  'The agents start from here. What they raise arrives in the feed, each item ' +
  'citing the row it came from, and nothing changes anywhere until you approve ' +
  'it.'
