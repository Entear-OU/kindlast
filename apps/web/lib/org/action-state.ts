/**
 * The shape a settings form action reports back (ENT-202).
 *
 * In its own module rather than beside the actions, and not for tidiness: a
 * `'use server'` file may export async functions and nothing else. Exporting
 * the `idle` value from there compiles, typechecks, lints and passes every
 * unit test, then fails at request time with "A 'use server' file can only
 * export async functions, found object" and renders a blank page.
 *
 * The type could have stayed, since types are erased before Next sees the
 * module. Keeping both together is the version that does not invite someone to
 * move the value back.
 */
export interface ActionState {
  status: 'idle' | 'ok' | 'error'
  message: string
}

export const idle: ActionState = { status: 'idle', message: '' }
