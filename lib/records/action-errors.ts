/**
 * Plain-language failures for the records server actions (ENT-166).
 *
 * The records actions used to return `error.message` straight from Supabase,
 * and the client rendered it. A founder submitting the ROPA form before
 * finishing onboarding was shown:
 *
 *     create_processing_activity: no compliance profile for user
 *
 * That is a `raise exception` string from a Postgres function. It names
 * internals, it reads as a crash, and it tells the founder nothing about what
 * to do. ENT-156 fixed exactly this shape on the billing page; this is the same
 * treatment for the records surface.
 *
 * Known conditions get copy that says what happened and what to do next.
 * Anything unrecognised collapses to one generic line, so a new database error
 * can never leak by default. The detail is logged server-side, where it is
 * useful, instead of shown to the user.
 */

const GENERIC = "We couldn't save that. Please try again, or contact support if it keeps happening."

/** Matched against the raw error text, most specific first. */
const KNOWN: ReadonlyArray<readonly [RegExp, string]> = [
  [
    /no compliance profile for user/i,
    'Finish onboarding first. Your records hang off the compliance profile it creates.',
  ],
  [
    /free plan|plan cap|cap reached|too many activities/i,
    'You have reached the Free plan limit. Upgrade to Pro to add more.',
  ],
  [/row-level security|permission denied|not authorized/i, "You don't have access to that record."],
]

/**
 * Turn a raw action failure into copy safe to render. Pass the original error
 * so it can be logged; only the return value should reach the UI.
 */
export function recordsActionError(raw: string, context?: string): string {
  for (const [pattern, message] of KNOWN) {
    if (pattern.test(raw)) return message
  }

  // Unrecognised: keep the detail server-side rather than in the founder's face.
  console.error(`[records] ${context ?? 'action'} failed: ${raw}`)
  return GENERIC
}
