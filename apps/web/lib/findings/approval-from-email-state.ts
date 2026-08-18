/**
 * What approving a finding from an email link reports back (ENT-249).
 *
 * Its own module rather than beside the action, for the reason every other
 * action-state module in this app carries in its header: a `'use server'` file
 * may export async functions and nothing else, and exporting the `idle` value
 * from there compiles, typechecks, lints, passes every unit test, and then
 * renders a blank page at request time.
 *
 * Separate from `FindingActionState` despite the family resemblance. That one
 * is the console acting on a finding it is already showing, so it never needs
 * to say which organisation anything happened in. This one runs for somebody
 * with no session, and where they go next is the answer core-api gave rather
 * than anywhere the request already knew about: §8's named failure is a
 * consultant with three clients acting against the wrong company from a stale
 * link, and a destination assembled on this side would be exactly that.
 */
export interface ApprovalFromEmailState {
  status: 'idle' | 'ok' | 'error'
  message: string
  /**
   * Where to send the person afterwards, `/o/{slug}/feed/{findingId}`.
   *
   * Built from the organisation the delegation named. Empty when the link was
   * refused, because a refused link has no organisation to name and offering
   * one would be inventing it.
   */
  destination?: string
}

export const idle: ApprovalFromEmailState = { status: 'idle', message: '' }
