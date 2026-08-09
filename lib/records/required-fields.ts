/**
 * Required-field guards for the manual record forms (ENT-168).
 *
 * Every manual "add" in Compliance records goes through a SECURITY DEFINER RPC
 * whose migration coalesces a blank name to a placeholder ("Untitled activity",
 * "Untitled system"). That fallback exists for Executor-written rows, which are
 * derived from an approved finding and always have something to show. A founder
 * pressing save on an empty form is a different case: it produced a junk row in
 * a statutory register that the app has no way to delete, and for a DSAR it
 * started a live 30-day Article 12(3) countdown for a request that never
 * existed.
 *
 * So the client disables the save button and the server action refuses the
 * write. Both matter: the button is the explanation, the action is the
 * guarantee.
 */

/** True when a founder-entered field is missing or only whitespace. */
export function isBlank(value: string | null | undefined): boolean {
  return (value ?? '').trim() === ''
}

export const REQUIRED_FIELD_MESSAGES = {
  activityName: 'Give the activity a name before saving.',
  dsarRequester: 'Name the requester before logging the request.',
  systemName: 'Give the system a name before saving.',
} as const
