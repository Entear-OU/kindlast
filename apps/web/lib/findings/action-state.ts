/**
 * What acting on a finding reports back (ENT-203).
 *
 * Its own module rather than beside the actions, and not for tidiness: a
 * `'use server'` file may export async functions and nothing else. Exporting
 * the `idle` value from there compiles, typechecks, lints and passes every unit
 * test, then fails at request time with "A 'use server' file can only export
 * async functions, found object" and renders a blank page. That happened once
 * already in ENT-202 and reached main.
 *
 * Separate from lib/org/action-state.ts despite the same shape, because this
 * one carries something that one does not: where the Executor put the record it
 * created. Merging them would mean the settings forms importing a field they
 * can never populate.
 */
export interface FindingActionState {
  status: 'idle' | 'ok' | 'error'
  message: string
  /**
   * Set when approving created a record, so the page can offer a link to it.
   *
   * Empty for every finding today: `action_type` is `review` until the corpus
   * is classified, so nothing is created (ENT-165).
   */
  createdRecordId?: string
  createdRecordTable?: string
}

export const idle: FindingActionState = { status: 'idle', message: '' }
