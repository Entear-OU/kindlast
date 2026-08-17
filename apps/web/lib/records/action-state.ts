/**
 * What writing to the compliance record reports back (ENT-200).
 *
 * Its own module rather than beside the actions, and not for tidiness: a
 * `'use server'` file may export async functions and nothing else. Exporting the
 * `idle` value from there compiles, typechecks, lints and passes every unit
 * test, then fails at request time with "A 'use server' file can only export
 * async functions, found object" and renders a blank page. That reached main
 * once already, in ENT-202.
 */
export interface RecordActionState {
  status: 'idle' | 'ok' | 'error'
  message: string
  /**
   * True when the server refused because a human has to confirm first.
   *
   * Carried as a flag rather than left for a client to detect from the message,
   * so the form can re-render with the confirmation visible instead of showing
   * a sentence about a checkbox the person cannot see. See `needsReview` in the
   * actions for why the flag is set on a code and not a string match.
   */
  needsReview?: boolean
}

export const idle: RecordActionState = { status: 'idle', message: '' }
