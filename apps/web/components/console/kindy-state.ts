/**
 * What one exchange with Kindy produced, as the composer draws it.
 *
 * Plain types in their own module so the client composer and the server
 * action can share them without either importing the other's runtime: the
 * composer must stay renderable in a test, and the action pulls in
 * `next/headers` the moment it is imported.
 *
 * The states are not collapsed, the ask panel's own rule: a refusal is a
 * guardrail working, a failure is something broken, no-model is a fact about
 * the deployment, and nothing-open is a fact about the organisation. Drawing
 * any of them as "sorry" would misreport the product's most important
 * behaviour.
 */
export type KindyState =
  | { status: 'idle' }
  | {
      status: 'answered'
      question: string
      answer: string
      /** The finding the answer is about: the subject a reader can check. */
      findingId: string
      findingTitle: string
    }
  | {
      status: 'refused'
      question: string
      reason: string
      findingId: string
      findingTitle: string
    }
  | {
      status: 'nothing-open'
      question: string
    }
  | { status: 'unavailable'; question: string }
  | { status: 'error'; question: string; message: string }

export const KINDY_IDLE: KindyState = { status: 'idle' }

export type KindyAction = (
  previous: KindyState,
  form: FormData,
) => Promise<KindyState>
