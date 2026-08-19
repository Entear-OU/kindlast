/**
 * Turning the Analyst's effort bucket into a sentence a person can read.
 *
 * `findings.effort_estimate` is the `effort_level` enum from 00002, and it
 * holds an order of magnitude rather than a quantity: `minutes`, `hours`,
 * `days`. `analyst_effort()` picks one from the kind of signal, so every
 * finding of a given kind gets the same bucket and no number is ever computed.
 *
 * The detail page interpolated that value straight into "Roughly {x} of work.",
 * which produced "Roughly days of work." on every profile-gap finding, and
 * would have produced "Roughly minutes of work." and "Roughly hours of work."
 * on the others. All three read as a missing word rather than as an estimate,
 * which is a bad look on a page whose whole argument is that a person can check
 * what it claims.
 *
 * A lookup rather than a clever pluraliser, because there are three values and
 * they are a closed set defined in the schema. If a fourth is ever added to
 * `effort_level`, add its line here: an unrecognised bucket renders nothing
 * rather than guessing, so the page loses a hint instead of printing a sentence
 * with a hole in it.
 */
const SENTENCES: Record<string, string> = {
  minutes: 'Minutes of work, roughly.',
  hours: 'Hours of work, roughly.',
  days: 'Days of work, roughly.',
}

/**
 * The sentence for a bucket, or null when there is nothing worth saying.
 *
 * Null rather than an empty string so a caller has to decide whether to render
 * the element at all, which is what stops an empty paragraph taking up the
 * space a real estimate would have.
 */
export function effortSentence(bucket: string | undefined): string | null {
  if (!bucket) return null
  return SENTENCES[bucket] ?? null
}
